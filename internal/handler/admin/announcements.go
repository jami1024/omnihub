package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

type announcementStore interface {
	List(ctx context.Context) ([]repository.Announcement, error)
	Create(ctx context.Context, a repository.Announcement) (int64, error)
	Update(ctx context.Context, id int64, a repository.Announcement) error
	Delete(ctx context.Context, id int64) error
}

func ListAnnouncementsHandler(store announcementStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := store.List(c.Request.Context())
		if err != nil {
			slog.Error("admin: list announcements failed", "err", err.Error())
			writeInternal(c, "could not list announcements")
			return
		}
		c.JSON(http.StatusOK, gin.H{"announcements": rows})
	}
}

func CreateAnnouncementHandler(store announcementStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in repository.Announcement
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if err := repository.ValidateAnnouncement(in); err != nil {
			writeBadRequest(c, err.Error())
			return
		}
		id, err := store.Create(c.Request.Context(), in)
		if err != nil {
			slog.Error("admin: create announcement failed", "err", err.Error())
			writeInternal(c, "could not create announcement")
			return
		}
		slog.Info("admin: announcement created", "id", id, "admin", adminActor(c))
		c.JSON(http.StatusCreated, gin.H{"id": id})
	}
}

func UpdateAnnouncementHandler(store announcementStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in repository.Announcement
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if err := repository.ValidateAnnouncement(in); err != nil {
			writeBadRequest(c, err.Error())
			return
		}
		if err := store.Update(c.Request.Context(), id, in); err != nil {
			if errors.Is(err, repository.ErrAnnouncementNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "announcement not found")
				return
			}
			slog.Error("admin: update announcement failed", "id", id, "err", err.Error())
			writeInternal(c, "could not update announcement")
			return
		}
		slog.Info("admin: announcement updated", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

func DeleteAnnouncementHandler(store announcementStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.Delete(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrAnnouncementNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "announcement not found")
				return
			}
			slog.Error("admin: delete announcement failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete announcement")
			return
		}
		slog.Info("admin: announcement deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}
