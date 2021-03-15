package main

import (
	"os"

	"github.com/diwise/api-pointofinterest/internal/pkg/application"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/logging"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
)

func main() {
	log := logging.NewLogger()
	sourceURL := os.Getenv("SOURCE_DATA_URL")

	db, err := database.NewDatabaseConnection(sourceURL, log)
	if err != nil {
		panic(err.Error())
	}

	application.CreateRouterAndStartServing(db, log)
}
