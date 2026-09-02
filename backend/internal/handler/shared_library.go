package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterSharedLibraryRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/shared-library/upload-policy", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.RequireSharedLibraryAccess(user); err != nil {
			failService(c, err)
			return
		}
		ok(c, service.SharedLibraryUploadPolicy())
	})
	r.GET("/shared-library/series", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		rows, err := svc.SharedAssetSeriesList(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"series": rows})
	})
	r.POST("/shared-library/series", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		row, err := svc.CreateSharedAssetSeries(user, req.Name)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"series": row})
	})
	r.PATCH("/shared-library/series/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		row, err := svc.UpdateSharedAssetSeries(user, c.Param("id"), req.Name)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"series": row})
	})
	r.DELETE("/shared-library/series/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteSharedAssetSeries(user, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
	r.GET("/shared-library/series/:id/assets", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		rows, err := svc.SharedAssets(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"assets": rows})
	})
	r.GET("/shared-library/assets", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		rows, err := svc.SharedAssets(user, "")
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"assets": rows})
	})
	r.POST("/shared-library/upload-batches", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		var req service.CreateSharedUploadBatchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		detail, err := svc.CreateSharedUploadBatch(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.GET("/shared-library/upload-batches/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.SharedUploadBatchDetail(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.GET("/shared-library/upload-batches/:id/events", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if _, err := svc.SharedUploadBatchDetail(user, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		streamSharedUploadEvents(c, svc, user, c.Param("id"))
	})
	r.POST("/shared-library/upload-batches/:id/renew", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.RenewSharedUploadBatch(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.PUT("/shared-library/upload-batches/:id/items/:itemId/content", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		policy := service.SharedLibraryUploadPolicy()
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, policy.ZIPMaxBytes+1)
		item, err := svc.UploadSharedItemContent(user, c.Param("id"), c.Param("itemId"), c.GetHeader("X-Upload-Token"), c.Request.Body)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"item": item})
	})
	r.POST("/shared-library/upload-batches/:id/items/:itemId/complete", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.CompleteSharedUploadItem(user, c.Param("id"), c.Param("itemId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.POST("/shared-library/upload-batches/:id/items/:itemId/retry", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.RetrySharedUploadItem(user, c.Param("id"), c.Param("itemId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.POST("/shared-library/upload-batches/:id/cancel", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.CancelSharedUploadBatch(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.PATCH("/shared-library/assets/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Title string `json:"title"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		asset, err := svc.UpdateSharedAsset(user, c.Param("id"), req.Title)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"asset": asset})
	})
	r.DELETE("/shared-library/assets/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteSharedAsset(user, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
	for path, thumbnail := range map[string]bool{"file": false, "thumbnail": true} {
		path, thumbnail := path, thumbnail
		r.GET("/shared-library/assets/:id/"+path, func(c *gin.Context) {
			user, err := currentUser(c, svc)
			if err != nil {
				failService(c, err)
				return
			}
			delivery, err := svc.PrepareSharedAssetDelivery(user, c.Param("id"), thumbnail, c.GetHeader("Range"))
			if err != nil {
				failService(c, err)
				return
			}
			if delivery.RedirectURL != "" {
				c.Header("Cache-Control", "private, no-store")
				c.Header("Referrer-Policy", "no-referrer")
				c.Redirect(http.StatusTemporaryRedirect, delivery.RedirectURL)
				return
			}
			resource := delivery.Resource
			stream, err := svc.OpenSharedAssetRange(user, c.Param("id"), thumbnail, c.GetHeader("Range"))
			if err != nil {
				failService(c, err)
				return
			}
			defer stream.Body.Close()
			c.Header("Cache-Control", "private, no-cache")
			c.Header("Accept-Ranges", stream.AcceptRanges)
			c.Header("X-Content-Type-Options", "nosniff")
			if stream.ContentRange != "" {
				c.Header("Content-Range", stream.ContentRange)
			}
			if stream.ContentLength >= 0 {
				c.Header("Content-Length", strconv.FormatInt(stream.ContentLength, 10))
			}
			c.DataFromReader(stream.StatusCode, stream.ContentLength, resource.MimeType, stream.Body, nil)
		})
	}
}

func streamSharedUploadEvents(c *gin.Context, svc *service.Service, user *model.User, batchID string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("X-Accel-Buffering", "no")
	flusher, okFlush := c.Writer.(http.Flusher)
	if !okFlush {
		fail(c, http.StatusInternalServerError, fmt.Errorf("SSE 不可用"))
		return
	}
	last := ""
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		detail, err := svc.SharedUploadBatchDetail(user, batchID)
		if err != nil {
			return
		}
		payload, _ := json.Marshal(detail)
		current := string(payload)
		if current != last {
			_, _ = fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", payload)
			flusher.Flush()
			last = current
		}
		switch detail.Batch.Status {
		case "completed", "completed_with_errors", "failed", "cancelled":
			return
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
