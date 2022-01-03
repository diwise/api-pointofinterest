package application

import (
	"encoding/json"
	"math"
	"time"

	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/diwise/messaging-golang/pkg/messaging"
	"github.com/diwise/messaging-golang/pkg/messaging/telemetry"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

func CreateWaterTempReceiver(db database.Datastore) messaging.TopicMessageHandler {
	return func(msg amqp.Delivery, logger zerolog.Logger) {

		logger.Info().Str("body", string(msg.Body)).Msg("message received from queue")

		telTemp := &telemetry.WaterTemperature{}
		err := json.Unmarshal(msg.Body, telTemp)

		if err != nil {
			logger.Error().Err(err).Msg("Failed to unmarshal message")
			return
		}

		if telTemp.Timestamp == "" {
			logger.Info().Msgf("Ignored water temperature message with an empty timestamp.")
			return
		}

		device := telTemp.Origin.Device
		temp := float64(math.Round(telTemp.Temp*10) / 10)
		observedAt, _ := time.Parse(time.RFC3339, telTemp.Timestamp)

		poi, err := db.UpdateWaterTemperatureFromDeviceID(device, temp, observedAt)
		if err == nil {
			logger.Info().Msgf("updated water temperature at %s to %f degrees", poi, temp)
		} else {
			logger.Error().Err(err).Msg("temperature update was ignored")
		}
	}
}
