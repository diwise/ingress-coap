package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/diwise/iot-agent/pkg/lwm2m"
	"github.com/diwise/service-chassis/pkg/infrastructure/env"
	"github.com/diwise/service-chassis/pkg/infrastructure/o11y/logging"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/coder"

	"golang.org/x/oauth2/clientcredentials"
)

func errorHandler(logger *slog.Logger) func(error) {
	return func(err error) {
		// filter out truncated message errors
		if !errors.Is(err, coder.ErrMessageTruncated) {
			logger.Error("coap error", "err", err.Error())
		}
	}
}

func loggingMiddleware(logger *slog.Logger) func(mux.Handler) mux.Handler {
	return func(next mux.Handler) mux.Handler {
		return mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
			// Ignore bots that scan for RFC 6690 endpoints
			path, err := r.Message.Options().Path()
			if err == nil && (path == ".well-known/core" || path == "/.well-known/core") {
				return
			}

			logger.Info(fmt.Sprintf("client address %v, %v", w.Conn().RemoteAddr(), r.String()))
			next.ServeCOAP(w, r)
		})
	}
}

func handleCoAP(logger *slog.Logger) func(mux.ResponseWriter, *mux.Message) {
	return func(w mux.ResponseWriter, req *mux.Message) {

		log := logger.With("message_id", req.MessageID())
		ctx := logging.NewContextWithLogger(req.Context(), log)

		bodySize, err := req.BodySize()
		if err != nil {
			log.Error("failed to get body size", "err", err.Error())
			return
		}

		if bodySize > 0 {
			var body []byte = make([]uint8, 256)
			n, err := req.Body().Read(body)
			if err != nil {
				log.Error("failed to read message body", "err", err.Error())
				return
			}

			err = decodePayload(ctx, body[0:n])
			if err != nil {
				log.Error("failed to decode payload", "err", err.Error())
				return
			}

		} else {
			log.Info("empty payload")
		}

		err = w.SetResponse(codes.Empty, message.TextPlain, nil)
		if err != nil {
			log.Error("unable to set response", "err", err.Error())
		}
	}
}

func handleHello(logger *slog.Logger) func(mux.ResponseWriter, *mux.Message) {
	return func(w mux.ResponseWriter, req *mux.Message) {
		err := w.SetResponse(codes.GET, message.TextPlain, bytes.NewReader([]byte("hello, world!")))
		if err != nil {
			logger.Error("unable to set response", "err", err.Error())
		}
	}
}

var logLevel = new(slog.LevelVar)

func main() {
	logLevel.Set(slog.LevelDebug)

	logger := slog.New(
		slog.NewJSONHandler(
			os.Stdout,
			&slog.HandlerOptions{Level: logLevel},
		),
	).With(
		slog.String("service", "ingress-coap"),
		slog.String("version", "v0.0.1"),
	)

	r := mux.NewRouter()
	r.Use(loggingMiddleware(logger))
	r.Handle("/coap", mux.HandlerFunc(handleCoAP(logger)))
	r.Handle("/hello", mux.HandlerFunc(handleHello(logger)))

	port := "5683"

	logger.Info("starting udp listener", "port", port)

	l, err := net.NewListenUDP("udp", ":"+port)
	if err != nil {
		logger.Error("failed to create udp listener", "err", err.Error())
		os.Exit(-1)
	}

	defer func() {
		l.Close()
	}()

	s := udp.NewServer(options.WithMux(r), options.WithErrors(errorHandler(logger)))

	err = s.Serve(l)
	if err != nil {
		logger.Error(
			"failed to start listening for incoming coap data",
			"port", port,
			"err", err.Error(),
		)
	}
}

const (
	TelegramTypeRegular         uint16 = 1
	TelegramTypeTwo             uint16 = 2
	TelegramMagicConstant40     uint8  = 0x40
	TelegramEndOfMeterDataToken uint16 = 0xAAAA
)

func decodePayload(ctx context.Context, payload []byte) error {
	var err error

	hex := fmt.Sprintf("%.2X", payload[0])
	payloadSize := len(payload)

	for i := 1; i < payloadSize; i++ {
		b := payload[i]
		hex = hex + fmt.Sprintf("%.2X", b)
	}

	logger := logging.GetFromContext(ctx)
	logger.Debug("received payload", "hex", hex, "bytecount", payloadSize)

	if payloadSize < 160 {
		err = errors.New("payload size is too small to contain a valid packet")
		logger.Error("decode failed", "err", err.Error())
		return err
	}

	telegramType := binary.LittleEndian.Uint16(payload[0:2])

	switch telegramType {
	case TelegramTypeRegular:
		obj, err := decodeRegularPayloadLwm2m(ctx, payload)
		if err != nil {
			logger.Error("decode failed", "err", err.Error())
			return err
		}

		err = pushLwm2mObject(ctx, obj)
		if err != nil {
			logger.Error("failed to push lwm2m object", "err", err.Error())
			return err
		}

	case TelegramTypeTwo:
		return decodeType2Payload(ctx, payload)
	default:
		err = fmt.Errorf("unknown telegram type %d", telegramType)
		logger.Error("decode failed", "err", err.Error())
		return err
	}

	return nil
}

func decodeType2Payload(ctx context.Context, payload []byte) (err error) {
	return nil
}

func decodeRegularPayloadLwm2m(ctx context.Context, payload []byte) (lwm2m.Lwm2mObject, error) {
	var err error
	logger := logging.GetFromContext(ctx)

	payloadSize := len(payload)

	if payload[2] != TelegramMagicConstant40 {
		err = fmt.Errorf("expected byte 2 to be %d", TelegramMagicConstant40)
		logger.Error("decode failed", "err", err.Error())
		return nil, err
	}

	telegramSize := binary.LittleEndian.Uint16(payload[3:5])
	if telegramSize != uint16(payloadSize) {
		err = fmt.Errorf("encoded telegram size %d != payload size %d", telegramSize, payloadSize)
		logger.Error("decode failed", "err", err.Error())
		return nil, err
	}

	idNumber := binary.LittleEndian.Uint32(payload[5:9])
	idString := fmt.Sprintf("%08x", idNumber)
	logger.Info(fmt.Sprintf("id number %s", idString))

	if binary.LittleEndian.Uint16(payload[146:150]) != TelegramEndOfMeterDataToken {
		err = errors.New("end of meter data token not found in expected position")
		logger.Error("decode failed", "err", err.Error())
		return nil, err
	}

	timeStamp := binary.LittleEndian.Uint32(payload[9:13])
	currentTime := time.Unix(int64(timeStamp), 0).UTC()
	totalVolume := binary.LittleEndian.Uint32(payload[14:18])
	logger.Info(fmt.Sprintf("total volume: %d litres @ %s", totalVolume, currentTime.Format(time.RFC3339)))

	timeStamp = binary.LittleEndian.Uint32(payload[26:30])
	lastMonthReferenceTime := time.Unix(int64(timeStamp), 0).UTC()
	lastMonthVolume := binary.LittleEndian.Uint32(payload[30:34])
	logger.Info(fmt.Sprintf("last month ref volume was %d @ %s", lastMonthVolume, lastMonthReferenceTime.Format(time.RFC3339)))

	flowRate := binary.LittleEndian.Uint32(payload[34:38])
	logger.Info(fmt.Sprintf("current flow rate %d litres / hour", flowRate))

	waterTemp := binary.LittleEndian.Uint32(payload[38:42])
	logger.Info(fmt.Sprintf("current water temp is %0.1f C", float32(waterTemp)/100.0))

	timeStamp = binary.LittleEndian.Uint32(payload[44:48])
	oldestReferenceTime := time.Unix(int64(timeStamp), 0).UTC()
	oldestVolume := binary.LittleEndian.Uint32(payload[48:52])
	logger.Info(fmt.Sprintf("oldest reference volume is %d from %s", oldestVolume, oldestReferenceTime.Format(time.RFC3339)))

	deltaIndex := 52
	deltaVolume := binary.LittleEndian.Uint16(payload[deltaIndex : deltaIndex+2])
	var accDeltaVolume uint16 = 0

	for deltaVolume != TelegramEndOfMeterDataToken {
		accDeltaVolume += deltaVolume

		deltaIndex += 2
		deltaVolume = binary.LittleEndian.Uint16(payload[deltaIndex : deltaIndex+2])
	}

	logger.Info(fmt.Sprintf("accumulated delta values: %d", accDeltaVolume))

	batteryLevel := uint32(payload[telegramSize-3])
	if batteryLevel <= 100 {
		logger.Info(fmt.Sprintf("battery level is %d%%", batteryLevel))
	} else {
		logger.Error(fmt.Sprintf("battery level has invalid value %d%%", batteryLevel))
	}

	fr := float64(flowRate) / 1000

	wm := lwm2m.NewWaterMeter(idString, float64(totalVolume)/1000, currentTime)
	wm.MinimumFlowRate = &fr
	wm.MaximumFlowRate = &fr

	return wm, err
}

func pushLwm2mObject(ctx context.Context, obj lwm2m.Lwm2mObject) error {
	var err error

	logger := logging.GetFromContext(ctx)

	agentURL := env.GetVariableOrDefault(ctx, "AGENT_URL", "")
	if agentURL == "" {
		logger.Debug("no agent url specified, skipping push")
		return nil
	}

	tokenURL := env.GetVariableOrDefault(ctx, "OAUTH2_TOKEN_URL", "")
	clientID := env.GetVariableOrDefault(ctx, "OAUTH2_CLIENT_ID", "")
	clientSecret := env.GetVariableOrDefault(ctx, "OAUTH2_CLIENT_SECRET", "")

	var oauthConfig *clientcredentials.Config

	if tokenURL != "" && clientID != "" && clientSecret != "" {
		oauthConfig = &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
		}
	}

	url := agentURL + "/api/v0/messages/lwm2m"

	body, err := json.Marshal(obj)
	if err != nil {
		err = fmt.Errorf("failed to marshal lwm2m object: %w", err)
		return err
	}

	reader := bytes.NewReader(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reader)
	if err != nil {
		err = fmt.Errorf("failed to create http request: %w", err)
		return err
	}
	req.Header.Add("Content-Type", "application/json")

	if oauthConfig == nil {
		token, err := oauthConfig.Token(ctx)
		if err != nil {
			err = fmt.Errorf("failed to get client credentials from %s: %w", oauthConfig.TokenURL, err)
			return err
		}

		req.Header.Add("Authorization", "Bearer "+token.AccessToken)
	}

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("failed to create device: %w", err)
		return err
	}
	defer req.Body.Close()
	io.Copy(io.Discard, req.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		err = fmt.Errorf("request failed, not authorized")
		return err
	}

	if resp.StatusCode != http.StatusCreated {
		err = fmt.Errorf("request failed with status code %d", resp.StatusCode)
		return err
	}

	return nil
}
