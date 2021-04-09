package application

import (
	"encoding/json"
	"math"
	"time"

	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/logging"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/iot-for-tillgenglighet/messaging-golang/pkg/messaging"
	"github.com/iot-for-tillgenglighet/messaging-golang/pkg/messaging/telemetry"
	"github.com/streadway/amqp"
)

func CreateWaterTempReceiver(db database.Datastore, log logging.Logger) messaging.TopicMessageHandler {
	return func(msg amqp.Delivery) {

		log.Infof("Message received from queue: %s", string(msg.Body))

		telTemp := &telemetry.WaterTemperature{}
		err := json.Unmarshal(msg.Body, telTemp)

		if err != nil {
			log.Error("Failed to unmarshal message")
			return
		}

		if telTemp.Timestamp == "" {
			log.Infof("Ignored water temperature message with an empty timestamp.")
			return
		}

		device := telTemp.Origin.Device
		temp := float64(math.Round(telTemp.Temp*10) / 10)
		observedAt, _ := time.Parse(time.RFC3339, telTemp.Timestamp)

		poi, err := db.UpdateWaterTemperatureFromDeviceID(device, temp, observedAt)
		if err == nil {
			log.Infof("updated water temperature at %s to %f degrees", poi, temp)
		} else {
			log.Infof("temperature update was ignored: %s", err.Error())
		}
	}
}
