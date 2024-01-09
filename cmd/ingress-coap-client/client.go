package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/udp"
)

var coapHost string
var coapTimeout string

var logLevel = new(slog.LevelVar)

func main() {

	flag.StringVar(&coapHost, "host", "", "Hostname or IP to the COAP server")
	flag.StringVar(&coapTimeout, "timeout", "5", "Timeout in seconds")
	flag.Parse()

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

	timeout, err := strconv.ParseUint(coapTimeout, 10, 64)
	if err != nil {
		logger.Error("invalid timeout value", "err", err.Error())
		os.Exit(-1)
	}

	co, err := udp.Dial(fmt.Sprintf("%s:5683", coapHost))

	if err != nil {
		logger.Error("error dialing to server", "err", err.Error())
		os.Exit(-1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	b, _ := hex.DecodeString("010040D00022765905C43ED46110000000000000000000000000738BCF610000000000000000F0D8FFFF0100D0A1D1610000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000AAAA85010000038200000000000000000000E6000800000090FF0314B3270000430002001700000004001A00002DFCA6EB190025EA4800E2FB540060919F")

	payload := bytes.NewReader(b)
	resp, err := co.Post(ctx, "/coap", message.AppOctets, payload)
	if err != nil {
		logger.Error("unable to get a response from coap server", "err", err.Error())
		return
	}

	logger.Info(fmt.Sprintf("response: %+v", resp))
}
