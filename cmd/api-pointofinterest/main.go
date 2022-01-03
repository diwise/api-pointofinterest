package main

import (
	"os"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/diwise/api-pointofinterest/internal/pkg/application"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/diwise/messaging-golang/pkg/messaging"
	"github.com/diwise/messaging-golang/pkg/messaging/telemetry"
)

func main() {

	serviceName := "api-pointofinterest"

	logger := log.With().Str("service", strings.ToLower(serviceName)).Logger()

	logger.Info().Msg("starting up ...")

	sourceURL := os.Getenv("SOURCE_DATA_URL")
	apiKey := os.Getenv("SOURCE_DATA_APIKEY")

	db, err := database.NewDatabaseConnection(sourceURL, apiKey, logger)
	if err != nil {
		panic(err.Error())
	}

	config := messaging.LoadConfiguration(serviceName, logger)
	messenger, _ := messaging.Initialize(config)
	defer messenger.Close()

	h := application.CreateWaterTempReceiver(db)
	messenger.RegisterTopicMessageHandler((&telemetry.WaterTemperature{}).TopicName(), h)

	application.CreateRouterAndStartServing(db, logger)
}
