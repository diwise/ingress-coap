package main

import (
	"bytes"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/plgd-dev/go-coap/v2"
	"github.com/plgd-dev/go-coap/v2/message"
	"github.com/plgd-dev/go-coap/v2/message/codes"
	"github.com/plgd-dev/go-coap/v2/mux"
)

func loggingMiddleware(next mux.Handler) mux.Handler {
	return mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		log.Info().Msgf("client address %v, %v\n", w.Client().RemoteAddr(), r.String())
		next.ServeCOAP(w, r)
	})
}

func handleCoAP(w mux.ResponseWriter, req *mux.Message) {
	if req.Body != nil {
		var body []byte = make([]uint8, 192)
		n, err := req.Body.Read(body)
		if err != nil {
			log.Error().Err(err).Msg("failed to read message body")
			return
		}

		hex := fmt.Sprintf("%X", body[0])
		for i := 1; i < n; i++ {
			b := body[i]
			hex = hex + fmt.Sprintf(" %X", b)
		}

		log.Info().Msgf("received payload: %s", hex)
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
	err := coap.ListenAndServe("udp", ":"+port, r)
	if err != nil {
		log.Fatal().Str("port", port).Err(err).Msg(
			"failed to start listening for incoming coap data",
		)
	}
}
