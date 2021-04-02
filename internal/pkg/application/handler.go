package application

import (
	"compress/flate"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/logging"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/iot-for-tillgenglighet/ngsi-ld-golang/pkg/datamodels/fiware"
	"github.com/iot-for-tillgenglighet/ngsi-ld-golang/pkg/ngsi-ld"
	ngsitypes "github.com/iot-for-tillgenglighet/ngsi-ld-golang/pkg/ngsi-ld/types"
	"github.com/rs/cors"
)

//RequestRouter needs a comment
type RequestRouter struct {
	impl *chi.Mux
}

func (router *RequestRouter) addNGSIHandlers(contextRegistry ngsi.ContextRegistry) {
	router.Get("/ngsi-ld/v1/entities/{entity}", ngsi.NewRetrieveEntityHandler(contextRegistry))
	router.Get("/ngsi-ld/v1/entities", ngsi.NewQueryEntitiesHandler(contextRegistry))
}

func (router *RequestRouter) addProbeHandlers() {
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

//Get accepts a pattern that should be routed to the handlerFn on a GET request
func (router *RequestRouter) Get(pattern string, handlerFn http.HandlerFunc) {
	router.impl.Get(pattern, handlerFn)
}

//Patch accepts a pattern that should be routed to the handlerFn on a PATCH request
func (router *RequestRouter) Patch(pattern string, handlerFn http.HandlerFunc) {
	router.impl.Patch(pattern, handlerFn)
}

//Post accepts a pattern that should be routed to the handlerFn on a POST request
func (router *RequestRouter) Post(pattern string, handlerFn http.HandlerFunc) {
	router.impl.Post(pattern, handlerFn)
}

func newRequestRouter() *RequestRouter {
	router := &RequestRouter{impl: chi.NewRouter()}

	router.impl.Use(cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
		Debug:            false,
	}).Handler)

	// Enable gzip compression for ngsi-ld responses
	compressor := middleware.NewCompressor(flate.DefaultCompression, "application/json", "application/ld+json")
	router.impl.Use(compressor.Handler)
	router.impl.Use(middleware.Logger)

	return router
}

func createRequestRouter(contextRegistry ngsi.ContextRegistry) *RequestRouter {
	router := newRequestRouter()

	router.addNGSIHandlers(contextRegistry)
	router.addProbeHandlers()

	return router
}

func createContextRegistry(db database.Datastore, log logging.Logger) ngsi.ContextRegistry {
	contextRegistry := ngsi.NewContextRegistry()
	ctxSource := contextSource{db: db, log: log}
	contextRegistry.Register(&ctxSource)
	return contextRegistry
}

//CreateRouterAndStartServing sets up the NGSI-LD router and starts serving incoming requests
func CreateRouterAndStartServing(db database.Datastore, log logging.Logger) {
	contextRegistry := createContextRegistry(db, log)
	router := createRequestRouter(contextRegistry)

	port := os.Getenv("SERVICE_PORT")
	if port == "" {
		port = "8080"
	}

	log.Infof("Starting api-pointofinterest on port %s.\n", port)
	log.Fatal(http.ListenAndServe(":"+port, router.impl))
}

type contextSource struct {
	db  database.Datastore
	log logging.Logger
}

func (cs *contextSource) ProvidesAttribute(attributeName string) bool {
	return false
}

func (cs *contextSource) ProvidesEntitiesWithMatchingID(entityID string) bool {
	return strings.HasPrefix(entityID, fiware.BeachIDPrefix)
}

func (cs *contextSource) ProvidesType(typeName string) bool {
	return typeName == "Beach"
}

func (cs *contextSource) GetEntities(query ngsi.Query, callback ngsi.QueryEntitiesCallback) error {
	pointsOfInterest, err := cs.db.GetAllFromType("Strandbad")
	if err != nil {
		return err
	}

	for _, poi := range pointsOfInterest {
		location := ngsitypes.CreateGeoJSONPropertyFromMultiPolygon(poi.Geometry.Lines)
		beach := fiware.NewBeach(poi.ID, poi.Name, location)

		if poi.SensorID != nil {
			sensor := fmt.Sprintf("%s%s", fiware.DeviceIDPrefix, *poi.SensorID)
			ref := ngsitypes.NewMultiObjectRelationship([]string{sensor})
			beach.RefSeeAlso = &ref
		}

		callback(beach.WithDescription(poi.Description))
	}

	return nil
}

func (cs *contextSource) RetrieveEntity(entityID string, request ngsi.Request) (ngsi.Entity, error) {

	// Remove urn:ngsi-ld:Beach prefix
	entityID = strings.TrimPrefix(entityID, fiware.BeachIDPrefix)

	poi, err := cs.db.GetFromID(entityID)
	if err != nil {
		return nil, err
	}

	location := ngsitypes.CreateGeoJSONPropertyFromMultiPolygon(poi.Geometry.Lines)
	beach := fiware.NewBeach(poi.ID, poi.Name, location).WithDescription(poi.Description)
	return beach, nil
}

func (cs *contextSource) CreateEntity(typeName, entityID string, request ngsi.Request) error {
	return errors.New("not implemented")
}

func (cs *contextSource) UpdateEntityAttributes(entityID string, request ngsi.Request) error {
	return errors.New("not implemented")
}
