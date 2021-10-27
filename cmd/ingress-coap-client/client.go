package main

import (
	"bytes"
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/plgd-dev/go-coap/v2/message"
	"github.com/plgd-dev/go-coap/v2/udp"
)

func main() {
	co, err := udp.Dial("127.0.0.1:5683")

	if err != nil {
		log.Fatal().Err(err).Msg("error dialing to server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := bytes.NewReader([]byte("hello, world!"))
	resp, err := co.Post(ctx, "/coap", message.AppOctets, payload)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to get a response from coap server")
		return
	}

	log.Info().Msgf("response: %+v", resp)
}
