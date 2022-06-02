package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/plgd-dev/go-coap/v2/message"
	"github.com/plgd-dev/go-coap/v2/udp"
)

func main() {
	co, err := udp.Dial("coap.diwise.io:5683")

	if err != nil {
		log.Fatal().Err(err).Msg("error dialing to server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	b, _ := hex.DecodeString("010040D00022765905C43ED46110000000000000000000000000738BCF610000000000000000F0D8FFFF0100D0A1D1610000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000AAAA85010000038200000000000000000000E6000800000090FF0314B3270000430002001700000004001A00002DFCA6EB190025EA4800E2FB540060919F")

	payload := bytes.NewReader(b)
	resp, err := co.Post(ctx, "/coap", message.AppOctets, payload)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to get a response from coap server")
		return
	}

	log.Info().Msgf("response: %+v", resp)
}
