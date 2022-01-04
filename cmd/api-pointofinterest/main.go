package main

import (
	"os"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/diwise/api-pointofinterest/internal/pkg/application"
	"github.com/diwise/api-pointofinterest/internal/pkg/application/services"
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
	trailStatusURL := os.Getenv("PREPARATION_STATUS_URL")

	db, err := database.NewDatabaseConnection(sourceURL, apiKey, logger)
	if err != nil {
		panic(err.Error())
	}

	config := messaging.LoadConfiguration(serviceName, logger)
	messenger, _ := messaging.Initialize(config)
	defer messenger.Close()

	h := application.CreateWaterTempReceiver(db)
	messenger.RegisterTopicMessageHandler((&telemetry.WaterTemperature{}).TopicName(), h)

	tps := services.NewTrailPreparationService(logger, trailStatusURL, db)
	defer tps.Shutdown()

	application.CreateRouterAndStartServing(db, logger)
}
