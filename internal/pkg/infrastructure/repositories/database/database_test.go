package database

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rs/zerolog/log"

	"github.com/matryer/is"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

var response = `{"type":"FeatureCollection","features":[
	{"id":1545,"type":"Feature",
	"properties":{
		"name":"Lillsjöns vinterbad","type":"Strandbad","created":"2020-06-04 14:26:58","updated":"2020-12-02 08:46:56","published":true,
		"fields":[
			{"id":153,"name":"Allmänt tillgänglig","type":"DROPDOWN","value":"Hela dygnet"},{"id":29,"name":"Sandstrand","type":"TOGGLE","value":"Nej"},{"id":30,"name":"Bergsstrand","type":"TOGGLE","value":"Nej"},{"id":33,"name":"Långgrunt","type":"TOGGLE","value":"Nej"},{"id":154,"name":"Bokningsbar","type":"TOGGLE","value":"Nej"},{"id":1,"name":"Beskrivning","type":"FREETEXT","value":"En beskrivning om stranden"},{"id":180,"name":"Kontakt länk","type":"FREETEXT","value":"https:\/\/www.facebook.com\/Badarna\/"},{"id":186,"name":"Felanmälan telefon","type":"FREETEXT","value":"060-XX XX XX"},{"id":187,"name":"Felanmälan e-post","type":"FREETEXT","value":"felanmelan@dev.null"},{"id":230,"name":"Temperatursensor","type":"FREETEXT","value":"sk-elt-temp-01"}
			]
	},
	"geometry":{
		"type":"MultiPolygon",
		"coordinates":[
			[
				[
					[17.472639624581532,62.43515222128755],
					[17.473786216868415,62.43536925652586],
					[17.474885857241564,62.43543825033344],
					[17.475474288890823,62.43457483981894],
					[17.47433409463916,62.43422493303495],
					[17.474073693177655,62.43422553227232],
					[17.473565135906316,62.4344799858447],
					[17.47299514306735,62.43493669748255],
					[17.472639624581532,62.43515222128755]
				]
			]
		]}}
	]}`

func TestDataLoad(t *testing.T) {
	mockServer := setupMockServiceThatReturns(200, response)
	url := mockServer.URL

	db, err := NewDatabaseConnection(url, "apikey", log.With().Logger())

	if err != nil {
		t.Errorf("Test failed: %s", err.Error())
		return
	}

	_, err = db.GetBeachFromID(SundsvallAnlaggningPrefix + "1545")
	if err != nil {
		t.Errorf("Failed to find entity in database")
		return
	}

	all, err := db.GetAllBeaches()
	if err != nil {
		t.Errorf("Failed to query all beaches")
		return
	}

	if len(all) != 1 {
		t.Errorf("Expected 1 but was %d published beaches", len(all))
	}
}

func TestThatNewDatabaseConnectionFailsOnEmptyApikey(t *testing.T) {
	is := is.New(t)

	_, err := NewDatabaseConnection("", "", log.With().Logger())

	is.True(err != nil) // NewDatabaseConnection should fail if apikey is left empty.
}

func setupMockServiceThatReturns(responseCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(responseCode)
		w.Header().Add("Content-Type", "application/json")
		if body != "" {
			w.Write([]byte(body))
		}
	}))
}
