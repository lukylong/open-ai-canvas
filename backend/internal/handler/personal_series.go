package handler

import (
	"github.com/gin-gonic/gin"
	"infinite-canvas/backend/internal/service"
	"net/http"
)

func RegisterPersonalSeriesRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/personal-series", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		state, err := svc.PersonalSeriesState(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, state)
	})
	save := func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Name     string `json:"name"`
			ParentID string `json:"parentId"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		row, err := svc.SavePersonalSeries(user, c.Param("id"), req.Name, req.ParentID)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"series": row})
	}
	r.POST("/personal-series", save)
	r.PATCH("/personal-series/:id", save)
	r.DELETE("/personal-series/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeletePersonalSeries(user, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
	r.POST("/personal-series/members", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
		var req struct {
			SeriesID string   `json:"seriesId"`
			AssetIDs []string `json:"assetIds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if err := svc.MovePersonalAssets(user, req.SeriesID, req.AssetIDs); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
	r.PUT("/personal-series/:id/cover", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			AssetID string `json:"assetId"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if err := svc.SetPersonalSeriesCover(user, c.Param("id"), req.AssetID); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
}
