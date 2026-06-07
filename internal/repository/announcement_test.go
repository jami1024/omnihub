package repository

import "testing"

func TestValidateAnnouncementRejectsUnknownKind(t *testing.T) {
	row := Announcement{Title: "T", Body: "B", Kind: "sale", Status: "draft", Placement: "portal_home"}
	if err := ValidateAnnouncement(row); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
}

func TestValidateAnnouncementAcceptsPublishedBanner(t *testing.T) {
	row := Announcement{Title: "T", Body: "B", Kind: "maintenance", Status: "published", Placement: "banner"}
	if err := ValidateAnnouncement(row); err != nil {
		t.Fatalf("expected valid announcement, got %v", err)
	}
}
