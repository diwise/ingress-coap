package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/plgd-dev/go-coap/v2"
	"github.com/plgd-dev/go-coap/v2/message"
	"github.com/plgd-dev/go-coap/v2/message/codes"
	"github.com/plgd-dev/go-coap/v2/mux"
)

func loggingMiddleware(next mux.Handler) mux.Handler {
	return mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		// Ignore bots that scan for RFC 6690 endpoints
		path, err := r.Message.Options.Path()
		if err == nil && path == ".well-known/core" {
			return
		}

		log.Info().Msgf("client address %v, %v", w.Client().RemoteAddr(), r.String())
		next.ServeCOAP(w, r)
	})
}

func handleCoAP(w mux.ResponseWriter, req *mux.Message) {
	if req.Body != nil {
		var body []byte = make([]uint8, 256)
		n, err := req.Body.Read(body)
		if err != nil {
			log.Error().Err(err).Msg("failed to read message body")
			return
		}

		decodePayload(body[0:n])

	} else {
		log.Info().Msg("empty payload")
	}

	err := w.SetResponse(codes.Empty, message.TextPlain, nil)
	if err != nil {
		log.Error().Err(err).Msg("unable to set response")
	}
}

func handleHello(w mux.ResponseWriter, req *mux.Message) {
	err := w.SetResponse(codes.GET, message.TextPlain, bytes.NewReader([]byte("hello, world!")))
	if err != nil {
		log.Error().Err(err).Msg("unable to set response")
	}
}

func main() {
	r := mux.NewRouter()
	r.Use(loggingMiddleware)
	r.Handle("/coap", mux.HandlerFunc(handleCoAP))
	r.Handle("/hello", mux.HandlerFunc(handleHello))

	port := "5683"

	log.Info().Msgf("starting udp listener on port %s", port)

	err := coap.ListenAndServe("udp", ":"+port, r)
	if err != nil {
		log.Fatal().Str("port", port).Err(err).Msg(
			"failed to start listening for incoming coap data",
		)
	}
}

const (
	TelegramTypeRegular         uint16 = 1
	TelegramMagicConstant40     uint8  = 0x40
	TelegramEndOfMeterDataToken uint16 = 0xAAAA
)

func decodePayload(payload []byte) {
	hex := fmt.Sprintf("%.2X", payload[0])
	payloadSize := len(payload)

	for i := 1; i < payloadSize; i++ {
		b := payload[i]
		hex = hex + fmt.Sprintf("%.2X", b)
	}

	log.Info().Msgf("received payload: %s (%d bytes)", hex, payloadSize)

	if payloadSize < 160 {
		log.Error().Msg("payload size is too small to contain a valid packet")
		return
	}

	telegramType := binary.LittleEndian.Uint16(payload[0:2])
	if telegramType != TelegramTypeRegular {
		log.Error().Msgf("unknown telegram type %d", telegramType)
		return
	}

	if payload[2] != TelegramMagicConstant40 {
		log.Error().Msgf("expected byte 2 to be %d", TelegramMagicConstant40)
		return
	}

	telegramSize := binary.LittleEndian.Uint16(payload[3:5])
	if telegramSize != uint16(payloadSize) {
		log.Error().Msgf("encoded telegram size %d != payload size %d", telegramSize, payloadSize)
		return
	}

	idNumber := binary.LittleEndian.Uint32(payload[5:9])
	log.Info().Msgf("id number %d", idNumber)

	if binary.LittleEndian.Uint16(payload[146:150]) != TelegramEndOfMeterDataToken {
		log.Error().Msg("end of meter data token not found in expected position")
		return
	}

	timeStamp := binary.LittleEndian.Uint32(payload[9:13])
	currentTime := time.Unix(int64(timeStamp), 0).UTC()
	log.Info().Msgf("current timestamp is %s", currentTime.Format(time.RFC3339))

	timeStamp = binary.LittleEndian.Uint32(payload[26:30])
	lastMonthReferenceTime := time.Unix(int64(timeStamp), 0).UTC()
	log.Info().Msgf("last month reference volume timestamp is %s", lastMonthReferenceTime.Format(time.RFC3339))

	timeStamp = binary.LittleEndian.Uint32(payload[44:48])
	oldestReferenceTime := time.Unix(int64(timeStamp), 0).UTC()
	log.Info().Msgf("oldest reference volume timestamp is %s", oldestReferenceTime.Format(time.RFC3339))

	batteryLevel := uint32(payload[telegramSize-3])
	if batteryLevel <= 100 {
		log.Info().Msgf("battery level is %d%%", batteryLevel)
	} else {
		log.Error().Msgf("battery level has invalid value %d%%", batteryLevel)
	}
}
