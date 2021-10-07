package main

import (
	"os"

	"github.com/diwise/api-pointofinterest/internal/pkg/application"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/logging"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/diwise/messaging-golang/pkg/messaging"
	"github.com/diwise/messaging-golang/pkg/messaging/telemetry"
)

func main() {

	serviceName := "api-pointofinterest"

	log := logging.NewLogger()
	log.Infof("Starting up %s ...", serviceName)

	sourceURL := os.Getenv("SOURCE_DATA_URL")
	apiKey := os.Getenv("SOURCE_DATA_APIKEY")

	db, err := database.NewDatabaseConnection(sourceURL, apiKey, log)
	if err != nil {
		panic(err.Error())
	}

	config := messaging.LoadConfiguration(serviceName)
	messenger, _ := messaging.Initialize(config)
	defer messenger.Close()

	messenger.RegisterTopicMessageHandler(
		(&telemetry.WaterTemperature{}).TopicName(),
		application.CreateWaterTempReceiver(db, log),
	)

	application.CreateRouterAndStartServing(db, log)
}
