package portal

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

type announcementSource interface {
	ListActive(ctx context.Context, placement string, now time.Time) ([]repository.Announcement, error)
}

func AnnouncementsHandler(store announcementSource) gin.HandlerFunc {
	return func(c *gin.Context) {
		placement := c.DefaultQuery("placement", "portal_home")
		if placement != "portal_home" && placement != "login" && placement != "banner" {
			writeBadRequest(c, "invalid placement")
			return
		}
		rows, err := store.ListActive(c.Request.Context(), placement, time.Now().UTC())
		if err != nil {
			slog.Error("portal: list announcements failed", "err", err.Error(), "placement", placement)
			writeInternal(c, "could not load announcements")
			return
		}
		c.JSON(http.StatusOK, gin.H{"announcements": rows})
	}
}
