package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestImportUsageStatisticsRejectsOversizedBody(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	prevMaxBytes := usageImportMaxBytes
	usageImportMaxBytes = 8
	t.Cleanup(func() {
		usageImportMaxBytes = prevMaxBytes
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/usage/import",
		strings.NewReader(`{"version":1,"usage":{"total_requests":1}}`),
	)

	h.ImportUsageStatistics(ginCtx)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}
