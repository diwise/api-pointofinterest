package application

import (
	"compress/flate"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/diwise/ngsi-ld-golang/pkg/datamodels/fiware"
	"github.com/diwise/ngsi-ld-golang/pkg/ngsi-ld"
	"github.com/diwise/ngsi-ld-golang/pkg/ngsi-ld/geojson"
	ngsitypes "github.com/diwise/ngsi-ld-golang/pkg/ngsi-ld/types"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
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

func createContextRegistry(db database.Datastore, logger zerolog.Logger) ngsi.ContextRegistry {
	contextRegistry := ngsi.NewContextRegistry()
	ctxSource := contextSource{db: db, logger: logger}
	contextRegistry.Register(&ctxSource)
	return contextRegistry
}

//CreateRouterAndStartServing sets up the NGSI-LD router and starts serving incoming requests
func CreateRouterAndStartServing(db database.Datastore, logger zerolog.Logger) {
	contextRegistry := createContextRegistry(db, logger)
	router := createRequestRouter(contextRegistry)

	port := os.Getenv("SERVICE_PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info().Str("port", port).Msg("listening for incoming connections")

	err := http.ListenAndServe(":"+port, router.impl)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to start listening on port")
	}
}

type contextSource struct {
	db     database.Datastore
	logger zerolog.Logger
}

func (cs *contextSource) ProvidesAttribute(attributeName string) bool {
	return false
}

func (cs *contextSource) ProvidesEntitiesWithMatchingID(entityID string) bool {
	return strings.HasPrefix(entityID, fiware.BeachIDPrefix)
}

func (cs *contextSource) GetProvidedTypeFromID(entityID string) (string, error) {
	if strings.HasPrefix(entityID, fiware.BeachIDPrefix) {
		return "Beach", nil
	}

	return "", fmt.Errorf("unknown entityID prefix")
}

func (cs *contextSource) ProvidesType(typeName string) bool {
	return typeName == "Beach"
}

func (cs *contextSource) GetEntities(query ngsi.Query, callback ngsi.QueryEntitiesCallback) error {
	pointsOfInterest, err := cs.db.GetAllBeaches()
	if err != nil {
		return err
	}

	for _, poi := range pointsOfInterest {
		location := geojson.CreateGeoJSONPropertyFromMultiPolygon(poi.Geometry.Lines)
		beach := fiware.NewBeach(poi.ID, poi.Name, location)

		references := []string{}

		if poi.SensorID != nil {
			sensor := fmt.Sprintf("%s%s", fiware.DeviceIDPrefix, *poi.SensorID)
			references = append(references, sensor)
		}

		if poi.NUTSCode != nil {
			references = append(references, fmt.Sprintf("https://badplatsen.havochvatten.se/badplatsen/karta/#/bath/%s", *poi.NUTSCode))
		}

		if poi.WikidataID != nil {
			references = append(references, fmt.Sprintf("https://www.wikidata.org/wiki/%s", *poi.WikidataID))
		}

		if len(references) > 0 {
			ref := ngsitypes.NewMultiObjectRelationship(references)
			beach.RefSeeAlso = &ref
		}

		if poi.WaterTemperature != nil {
			beach.WaterTemperature = ngsitypes.NewNumberProperty(*poi.WaterTemperature)
		}

		if !poi.DateCreated.IsZero() {
			beach.DateCreated = ngsitypes.CreateDateTimeProperty(poi.DateCreated.Format(time.RFC3339))
		}

		if !poi.DateModified.IsZero() {
			beach.DateModified = ngsitypes.CreateDateTimeProperty(poi.DateModified.Format(time.RFC3339))
		}

		callback(beach.WithDescription(poi.Description))
	}

	return nil
}

func (cs *contextSource) RetrieveEntity(entityID string, request ngsi.Request) (ngsi.Entity, error) {

	// Remove urn:ngsi-ld:Beach prefix
	entityID = strings.TrimPrefix(entityID, fiware.BeachIDPrefix)

	poi, err := cs.db.GetBeachFromID(entityID)
	if err != nil {
		return nil, err
	}

	location := geojson.CreateGeoJSONPropertyFromMultiPolygon(poi.Geometry.Lines)
	beach := fiware.NewBeach(poi.ID, poi.Name, location).WithDescription(poi.Description)
	return beach, nil
}

func (cs *contextSource) CreateEntity(typeName, entityID string, request ngsi.Request) error {
	return errors.New("not implemented")
}

func (cs *contextSource) UpdateEntityAttributes(entityID string, request ngsi.Request) error {
	return errors.New("not implemented")
}
