package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/plgd-dev/go-coap/v3"
	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
)

func loggingMiddleware(next mux.Handler) mux.Handler {
	return mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		// Ignore bots that scan for RFC 6690 endpoints
		path, err := r.Message.Options().Path()
		if err == nil && (path == ".well-known/core" || path == "/.well-known/core") {
			return
		}

		log.Info().Msgf("client address %v, %v", w.Conn().RemoteAddr(), r.String())
		next.ServeCOAP(w, r)
	})
}

func handleCoAP(w mux.ResponseWriter, req *mux.Message) {
	bodySize, err := req.BodySize()
	if err != nil {
		log.Error().Err(err).Msg("failed to get body size")
		return
	}

	if bodySize > 0 {
		var body []byte = make([]uint8, 256)
		n, err := req.Body().Read(body)
		if err != nil {
			log.Error().Err(err).Msg("failed to read message body")
			return
		}

		decodePayload(body[0:n])

	} else {
		log.Info().Msg("empty payload")
	}

	err = w.SetResponse(codes.Empty, message.TextPlain, nil)
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

	log.Debug().Msgf("received payload: %s (%d bytes)", hex, payloadSize)

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
	idString := fmt.Sprintf("%08x", idNumber)
	log.Info().Msgf("id number %s", idString)

	if binary.LittleEndian.Uint16(payload[146:150]) != TelegramEndOfMeterDataToken {
		log.Error().Msg("end of meter data token not found in expected position")
		return
	}

	timeStamp := binary.LittleEndian.Uint32(payload[9:13])
	currentTime := time.Unix(int64(timeStamp), 0).UTC()
	totalVolume := binary.LittleEndian.Uint32(payload[14:18])
	log.Info().Msgf("total volume: %d litres @ %s", totalVolume, currentTime.Format(time.RFC3339))

	timeStamp = binary.LittleEndian.Uint32(payload[26:30])
	lastMonthReferenceTime := time.Unix(int64(timeStamp), 0).UTC()
	lastMonthVolume := binary.LittleEndian.Uint32(payload[30:34])
	log.Info().Msgf("last month ref volume was %d @ %s", lastMonthVolume, lastMonthReferenceTime.Format(time.RFC3339))

	flowRate := binary.LittleEndian.Uint32(payload[34:38])
	log.Info().Msgf("current flow rate %d litres / hour", flowRate)

	waterTemp := binary.LittleEndian.Uint32(payload[38:42])
	log.Info().Msgf("current water temp is %d C", waterTemp)

	timeStamp = binary.LittleEndian.Uint32(payload[44:48])
	oldestReferenceTime := time.Unix(int64(timeStamp), 0).UTC()
	oldestVolume := binary.LittleEndian.Uint32(payload[48:52])
	log.Info().Msgf("oldest reference volume is %d from %s", oldestVolume, oldestReferenceTime.Format(time.RFC3339))

	deltaIndex := 52
	deltaVolume := binary.LittleEndian.Uint16(payload[deltaIndex : deltaIndex+2])
	var accDeltaVolume uint16 = 0

	for deltaVolume != TelegramEndOfMeterDataToken {
		accDeltaVolume += deltaVolume

		deltaIndex += 2
		deltaVolume = binary.LittleEndian.Uint16(payload[deltaIndex : deltaIndex+2])
	}

	log.Info().Msgf("accumulated delta values: %d", accDeltaVolume)

	batteryLevel := uint32(payload[telegramSize-3])
	if batteryLevel <= 100 {
		log.Info().Msgf("battery level is %d%%", batteryLevel)
	} else {
		log.Error().Msgf("battery level has invalid value %d%%", batteryLevel)
	}
}
