package cancelled_request_commits

import (
	"context"
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/persistence"
	httpapi "icecoreacclimationgate/internal/transport/http"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCancelledCreateDoesNotCommit(t *testing.T) {
	store, err := persistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chain, err := audit.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httpapi.New(application.New(store, chain))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/acclimation-cases", strings.NewReader(`{"request_id":"cancelled-create","opened_by":"operator","storage_temperature_c":-35,"specimen_tubes":[{"tube_id":"T1","label":"core-1"}]}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)

	if cases := server.App.Store.List(); len(cases) != 0 {
		t.Fatalf("TestCancelledCreateDoesNotCommit: cancelled request persisted %d case(s), response status=%d", len(cases), response.Code)
	}
}
