package handler

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterGeneratedMediaAnalyticsRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/internal/v1/analytics/materials", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		secret := strings.TrimSpace(os.Getenv("CANVAS_ANALYTICS_READ_TOKEN"))
		provided, hasBearer := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if len(secret) < 32 || !hasBearer || len(provided) != len(secret) || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		result, err := svc.GlobalGeneratedMediaAnalytics(c.Query("from"), c.Query("to"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}
