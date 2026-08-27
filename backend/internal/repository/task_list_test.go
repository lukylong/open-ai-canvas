package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTasksLoadsLogicalModelIDForPublicDisplay(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-list-logical-model?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	input := model.Task{
		ID:                     "TASK_LOGICAL_MODEL",
		UserID:                 "USER_LOGICAL_MODEL",
		Type:                   "canvas_image",
		Status:                 model.TaskStatusSucceeded,
		LogicalModelID:         "LOGICAL_MODEL_PUBLIC",
		LogicalModelRevisionID: "LOGICAL_MODEL_REVISION_PRIVATE",
		RouteID:                "ROUTE_PRIVATE",
		ChannelModelID:         "CHANNEL_MODEL_PRIVATE",
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := db.Create(&input).Error; err != nil {
		t.Fatal(err)
	}

	tasks, err := New(db).Tasks(input.UserID, 10, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("Tasks() returned %d tasks, want 1", len(tasks))
	}
	if tasks[0].LogicalModelID != input.LogicalModelID {
		t.Fatalf("LogicalModelID = %q, want %q", tasks[0].LogicalModelID, input.LogicalModelID)
	}
	if tasks[0].LogicalModelRevisionID != "" || tasks[0].RouteID != "" || tasks[0].ChannelModelID != "" {
		t.Fatalf("protected routing fields were loaded: %#v", tasks[0])
	}
}
