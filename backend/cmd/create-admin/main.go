package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

func main() {
	username := flag.String("username", strings.TrimSpace(os.Getenv("CANVAS_ADMIN_USERNAME")), "管理员用户名")
	password := flag.String("password", strings.TrimSpace(os.Getenv("CANVAS_ADMIN_PASSWORD")), "管理员密码（也可使用 CANVAS_ADMIN_PASSWORD）")
	email := flag.String("email", strings.TrimSpace(os.Getenv("CANVAS_ADMIN_EMAIL")), "管理员邮箱（可选）")
	displayName := flag.String("display-name", "管理员", "显示名称")
	promote := flag.Bool("promote-existing", false, "将同名现有账号提升为管理员并更新密码")
	targetDriver := flag.String("target-driver", env("CANVAS_DATABASE_DRIVER", "postgres"), "数据库驱动")
	targetDSN := flag.String("target-dsn", os.Getenv("DATABASE_URL"), "数据库 DSN")
	dataDir := flag.String("data-dir", env("CANVAS_BACKEND_DATA_DIR", "data"), "Canvas 数据目录")
	flag.Parse()

	*username = strings.TrimSpace(*username)
	if !usernamePattern.MatchString(*username) {
		log.Fatal("用户名必须为 3-32 位字母、数字、下划线或连字符")
	}
	if len(*password) < 8 {
		log.Fatal("密码至少 8 位")
	}
	db, err := database.Open(database.Config{Driver: *targetDriver, DSN: strings.TrimSpace(*targetDSN), DataDir: *dataDir})
	if err != nil {
		log.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		log.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now()
	var existing model.User
	err = db.Where("lower(username) = lower(?)", *username).First(&existing).Error
	if err == nil {
		if !*promote {
			log.Fatal("同名账号已存在；确认后使用 --promote-existing")
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			updates := map[string]any{"role": model.UserRoleAdmin, "status": model.UserStatusActive, "password_hash": string(hash), "updated_at": now}
			if strings.TrimSpace(*email) != "" {
				updates["email"] = strings.ToLower(strings.TrimSpace(*email))
			}
			if err := tx.Model(&model.User{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return err
			}
			return tx.Delete(&model.AuthSession{}, "user_id = ?", existing.ID).Error
		})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ADMIN_UPDATED username=%s sessions_revoked=true\n", existing.Username)
		return
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Fatal(err)
	}
	user := model.User{ID: uuid.NewString(), Username: *username, Email: strings.ToLower(strings.TrimSpace(*email)), DisplayName: strings.TrimSpace(*displayName), SourceSystem: "canvas", Role: model.UserRoleAdmin, Status: model.UserStatusActive, PasswordHash: string(hash), CreatedAt: now, UpdatedAt: now}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	if err := db.Create(&user).Error; err != nil {
		log.Fatal(err)
	}
	svc := service.New(repository.New(db), *dataDir)
	if err := svc.GrantSignupBonusForMigratedUser(user.ID); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ADMIN_CREATED username=%s\n", user.Username)
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
