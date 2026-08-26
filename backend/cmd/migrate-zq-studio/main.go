package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/zqmigration"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		log.Fatal("用法: migrate-zq-studio inventory|backfill|follow|verify|storage [flags]")
	}
	command := strings.ToLower(strings.TrimSpace(os.Args[1]))
	switch command {
	case "inventory", "backfill", "follow", "verify", "storage":
	default:
		log.Fatalf("未知命令 %q，可用 inventory|backfill|follow|verify|storage", command)
	}
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	sourceEnvPath := flags.String("source-env", os.Getenv("ZQ_SOURCE_ENV"), "ZQ 环境文件路径（只读）")
	sourceDSNFlag := flags.String("source-dsn", os.Getenv("ZQ_DATABASE_URL"), "ZQ PostgreSQL DSN")
	targetDSN := flags.String("target-dsn", os.Getenv("DATABASE_URL"), "Canvas 数据库 DSN")
	targetDriver := flags.String("target-driver", env("CANVAS_DATABASE_DRIVER", "postgres"), "Canvas 数据库驱动")
	dataDir := flags.String("data-dir", env("CANVAS_BACKEND_DATA_DIR", "data"), "Canvas 数据目录")
	interval := flags.Duration("interval", 5*time.Second, "follow 轮询间隔")
	overlap := flags.Duration("overlap", 5*time.Minute, "follow 重叠窗口")
	once := flags.Bool("once", false, "follow 仅执行一轮")
	storageActorUserID := flags.String("storage-actor-user-id", "", "记录平台存储配置变更的启用管理员 ID（默认首个启用管理员）")
	storagePathPrefix := flags.String("storage-path-prefix", "canvas", "影策新资源的 COS 路径前缀")
	replacePlatformStorage := flags.Bool("replace-platform-storage", false, "显式覆盖已存在的平台存储配置")
	_ = flags.Parse(os.Args[2:])

	sourceEnv := map[string]string{}
	if strings.TrimSpace(*sourceEnvPath) != "" {
		loaded, err := loadEnvFile(*sourceEnvPath)
		if err != nil {
			log.Fatalf("读取 ZQ 环境文件失败: %v", err)
		}
		sourceEnv = loaded
	}
	target, err := database.Open(database.Config{Driver: *targetDriver, DSN: strings.TrimSpace(*targetDSN), DataDir: *dataDir})
	if err != nil {
		log.Fatalf("连接 Canvas 数据库失败: %v", err)
	}
	if err := database.MigrateSchema(target); err != nil {
		log.Fatalf("升级 Canvas 数据结构失败: %v", err)
	}
	target = target.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	cosConfig := sourceCOSConfig(sourceEnv)
	if command == "storage" {
		result, err := zqmigration.New(nil, target, *dataDir, cosConfig).ImportPlatformStorage(zqmigration.PlatformStorageOptions{
			ActorUserID: *storageActorUserID,
			PathPrefix:  *storagePathPrefix,
			Replace:     *replacePlatformStorage,
		})
		printResult(result, err)
		return
	}

	sourceDSN := firstNonEmpty(*sourceDSNFlag, sourceEnv["DATABASE_URL"])
	sourceDSN = normalizeDSN(sourceDSN)
	if sourceDSN == "" {
		log.Fatal("缺少 ZQ_DATABASE_URL、--source-dsn 或 source env 中的 DATABASE_URL")
	}

	source, err := gorm.Open(postgres.Open(readOnlyPostgresDSN(sourceDSN)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("连接 ZQ PostgreSQL 失败: %v", err)
	}
	if err := source.Exec("SET default_transaction_read_only = on").Error; err != nil {
		log.Fatalf("设置 ZQ 只读会话失败: %v", err)
	}
	var readOnly string
	if err := source.Raw("SHOW default_transaction_read_only").Scan(&readOnly).Error; err != nil || !strings.EqualFold(strings.TrimSpace(readOnly), "on") {
		log.Fatalf("ZQ 数据库连接未进入只读模式: value=%q error=%v", readOnly, err)
	}
	migrator := zqmigration.New(source, target, *dataDir, cosConfig)

	switch command {
	case "inventory":
		inventory, err := migrator.Inventory()
		printResult(inventory, err)
	case "backfill":
		stats, err := migrator.Backfill()
		printResult(stats, err)
	case "verify":
		verification, err := migrator.Verify()
		if err == nil {
			for _, missing := range verification.Missing {
				if missing > 0 {
					err = fmt.Errorf("仍有未映射的 ZQ 记录")
					break
				}
			}
		}
		if err == nil {
			for _, conflicts := range verification.Conflicts {
				if conflicts > 0 {
					err = fmt.Errorf("仍有未解决的 ZQ 迁移冲突")
					break
				}
			}
		}
		printResult(verification, err)
	case "follow":
		for {
			watermark, err := migrator.LastSuccessfulWatermark()
			if err != nil {
				log.Fatal(err)
			}
			if !watermark.IsZero() {
				watermark = watermark.Add(-*overlap)
			}
			stats, err := migrator.RunOnce(watermark)
			if err != nil {
				log.Fatalf("增量迁移失败: %v", err)
			}
			printJSON(stats)
			if *once {
				return
			}
			time.Sleep(*interval)
		}
	}
}

func sourceCOSConfig(values map[string]string) zqmigration.COSConfig {
	publicRead, _ := strconv.ParseBool(strings.TrimSpace(values["QCLOUD_COS_PUBLIC_READ"]))
	return zqmigration.COSConfig{
		Bucket:           values["QCLOUD_COS_BUCKET"],
		Region:           values["QCLOUD_COS_REGION"],
		Domain:           values["QCLOUD_COS_DOMAIN"],
		InternalEndpoint: values["QCLOUD_COS_INTERNAL_ENDPOINT"],
		AccessKeyID:      firstNonEmpty(values["QCLOUD_COS_SECRET_ID"], values["QCLOUD_COS_ACCESS_KEY"]),
		AccessKeySecret:  values["QCLOUD_COS_SECRET_KEY"],
		PublicRead:       publicRead,
	}
}

func loadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values, scanner.Err()
}

func normalizeDSN(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"postgresql+asyncpg://", "postgres+asyncpg://", "postgresql+psycopg://"} {
		if strings.HasPrefix(value, prefix) {
			return "postgresql://" + strings.TrimPrefix(value, prefix)
		}
	}
	return value
}

// readOnlyPostgresDSN applies the safety setting during every PostgreSQL
// connection handshake, rather than relying on SET for one pooled connection.
func readOnlyPostgresDSN(value string) string {
	value = normalizeDSN(value)
	parsed, err := url.Parse(value)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		options := strings.TrimSpace(query.Get("options"))
		if !strings.Contains(strings.ToLower(options), "default_transaction_read_only") {
			options = strings.TrimSpace(options + " -c default_transaction_read_only=on")
		}
		query.Set("options", options)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if strings.TrimSpace(value) == "" || strings.Contains(strings.ToLower(value), "default_transaction_read_only") {
		return value
	}
	return value + " options='-c default_transaction_read_only=on'"
}

func printResult(value any, err error) {
	printJSON(value)
	if err != nil {
		log.Fatal(err)
	}
}

func printJSON(value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(raw))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
