package application

import (
	"compress/flate"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/diwise/api-pointofinterest/internal/pkg/domain"
	"github.com/diwise/api-pointofinterest/internal/pkg/infrastructure/repositories/database"
	"github.com/diwise/ngsi-ld-golang/pkg/datamodels/diwise"
	"github.com/diwise/ngsi-ld-golang/pkg/datamodels/fiware"
	"github.com/diwise/ngsi-ld-golang/pkg/ngsi-ld"
	"github.com/diwise/ngsi-ld-golang/pkg/ngsi-ld/geojson"
	ngsitypes "github.com/diwise/ngsi-ld-golang/pkg/ngsi-ld/types"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog"
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

	logger := httplog.NewLogger("api-pointofinterest", httplog.Options{
		JSON: true,
	})
	router.impl.Use(httplog.RequestLogger(logger))

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
	return strings.HasPrefix(entityID, fiware.BeachIDPrefix) || strings.HasPrefix(entityID, diwise.ExerciseTrailIDPrefix)
}

func (cs *contextSource) GetProvidedTypeFromID(entityID string) (string, error) {
	if strings.HasPrefix(entityID, fiware.BeachIDPrefix) {
		return fiware.BeachTypeName, nil
	}

	if strings.HasPrefix(entityID, diwise.ExerciseTrailIDPrefix) {
		return diwise.ExerciseTrailTypeName, nil
	}

	return "", fmt.Errorf("unknown entity id prefix")
}

func (cs *contextSource) ProvidesType(typeName string) bool {
	return typeName == fiware.BeachTypeName || typeName == diwise.ExerciseTrailTypeName
}

func (cs *contextSource) GetEntities(query ngsi.Query, callback ngsi.QueryEntitiesCallback) error {
	var err error

	for _, entityType := range query.EntityTypes() {
		if entityType == fiware.BeachTypeName {
			err = cs.getBeaches(query, callback)
		} else if entityType == diwise.ExerciseTrailTypeName {
			err = cs.getTrails(query, callback)
		}

		if err != nil {
			break
		}
	}

	return err
}

func (cs *contextSource) getBeaches(query ngsi.Query, callback ngsi.QueryEntitiesCallback) error {
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

func (cs *contextSource) getTrails(query ngsi.Query, callback ngsi.QueryEntitiesCallback) error {
	allTrails, err := cs.db.GetAllTrails()
	if err != nil {
		return err
	}

	for _, t := range allTrails {
		trail := convertDBTrailToFiwareExerciseTrail(t)
		callback(trail)
	}

	return nil
}

func (cs *contextSource) RetrieveEntity(entityID string, request ngsi.Request) (ngsi.Entity, error) {

	if strings.HasPrefix(entityID, fiware.BeachIDPrefix) {
		// Remove urn:ngsi-ld:Beach prefix
		entityID = strings.TrimPrefix(entityID, fiware.BeachIDPrefix)

		poi, err := cs.db.GetBeachFromID(entityID)
		if err != nil {
			return nil, err
		}

		location := geojson.CreateGeoJSONPropertyFromMultiPolygon(poi.Geometry.Lines)
		beach := fiware.NewBeach(poi.ID, poi.Name, location).WithDescription(poi.Description)
		return beach, nil
	} else if strings.HasPrefix(entityID, diwise.ExerciseTrailIDPrefix) {
		// Remove urn:ngsi-ld:ExerciseTrail prefix
		entityID = strings.TrimPrefix(entityID, diwise.ExerciseTrailIDPrefix)

		dbTrail, err := cs.db.GetTrailFromID(entityID)
		if err != nil {
			return nil, err
		}

		trail := convertDBTrailToFiwareExerciseTrail(*dbTrail)

		return trail, nil
	}

	return nil, fmt.Errorf("entity %s not found", entityID)
}

func (cs *contextSource) CreateEntity(typeName, entityID string, request ngsi.Request) error {
	return errors.New("not implemented")
}

func (cs *contextSource) UpdateEntityAttributes(entityID string, request ngsi.Request) error {
	return errors.New("not implemented")
}

func convertDBTrailToFiwareExerciseTrail(trail domain.ExerciseTrail) *diwise.ExerciseTrail {
	location := geojson.CreateGeoJSONPropertyFromLineString(trail.Geometry.Lines)
	exerciseTrail := diwise.NewExerciseTrail(trail.ID, trail.Name, trail.Length, trail.Description, location)

	if !trail.DateCreated.IsZero() {
		exerciseTrail.DateCreated = ngsitypes.CreateDateTimeProperty(trail.DateCreated.Format(time.RFC3339))
	}

	if !trail.DateModified.IsZero() {
		exerciseTrail.DateModified = ngsitypes.CreateDateTimeProperty(trail.DateModified.Format(time.RFC3339))
	}

	if !trail.DateLastPrepared.IsZero() {
		exerciseTrail.DateLastPreparation = ngsitypes.CreateDateTimeProperty(trail.DateLastPrepared.Format(time.RFC3339))
	}

	if trail.Source != "" {
		exerciseTrail.Source = ngsitypes.NewTextProperty(trail.Source)
	}

	return exerciseTrail
}
