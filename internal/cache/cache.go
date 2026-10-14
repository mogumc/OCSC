// Package cache 把「题目 → 答案」的映射持久化到 SQLite，
// 让同一道题重复提问时不必再消耗模型额度。
//
// 键由题干、选项、题型和图片共同决定，任意一项不同都视为不同题目。
package cache

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	// 纯 Go 实现的 SQLite 驱动：不需要 CGO，因此交叉编译与
	// distroless 静态镜像依然可用。
	_ "modernc.org/sqlite"

	"ocs-ai-proxy/internal/model"
)

// Entry 是一条缓存记录。
type Entry struct {
	Answer   string
	Analysis string
}

// Cache 是 SQLite 支撑的答案缓存，可安全并发使用。
type Cache struct {
	db *sql.DB
}

// schema 与参考实现保持一致的列名，方便互相迁移；
// question_hash 声明了 UNIQUE，SQLite 会自动建索引，无需再单独建。
const schema = `
CREATE TABLE IF NOT EXISTS answer_cache (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	question_hash TEXT    NOT NULL UNIQUE,
	title         TEXT    NOT NULL,
	options       TEXT,
	question_type TEXT,
	answer        TEXT    NOT NULL,
	analysis      TEXT,
	created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`

// Open 打开（必要时创建）缓存库。
func Open(path string) (*Cache, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("缓存文件路径不能为空")
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建缓存目录失败: %w", err)
		}
	}

	// 参数放在 DSN 里，这样连接池每次新建连接都会带上，不会丢设置
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开缓存库失败: %w", err)
	}

	// SQLite 写入是串行的，单连接可以彻底避开 database is locked
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	c := &Cache{db: db}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化缓存表失败: %w", err)
	}
	return c, nil
}

// Close 关闭缓存库。
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Key 计算题目内容的缓存键。
//
// 传入的应当是清洗过的请求（题干已去空白、选项已按行规整），
// 否则同一道题因为排版差异会算成两个键。
func Key(req *model.SearchRequest) string {
	var b strings.Builder
	b.WriteString(req.Title)
	b.WriteByte('|')
	b.WriteString(req.Options)
	b.WriteByte('|')
	b.WriteString(req.Type)
	for _, img := range req.Images {
		b.WriteByte('|')
		b.WriteString(img)
	}

	// 缓存键不需要抗碰撞攻击，用 MD5 足够且更快
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Get 查询缓存。未命中返回 (nil, nil)。
func (c *Cache) Get(key string) (*Entry, error) {
	var entry Entry
	err := c.db.QueryRow(
		`SELECT answer, COALESCE(analysis, '') FROM answer_cache WHERE question_hash = ?`,
		key,
	).Scan(&entry.Answer, &entry.Analysis)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("查询缓存失败: %w", err)
	}
	return &entry, nil
}

// Put 写入缓存；同一道题再次写入会覆盖旧答案。
func (c *Cache) Put(key string, req *model.SearchRequest, entry *Entry) error {
	_, err := c.db.Exec(`
		INSERT INTO answer_cache (question_hash, title, options, question_type, answer, analysis)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(question_hash) DO UPDATE SET
			answer     = excluded.answer,
			analysis   = excluded.analysis,
			created_at = CURRENT_TIMESTAMP
	`, key, req.Title, req.Options, req.Type, entry.Answer, entry.Analysis)
	if err != nil {
		return fmt.Errorf("写入缓存失败: %w", err)
	}
	return nil
}

// Count 返回已缓存的题目数量，仅用于启动时展示。
func (c *Cache) Count() (int, error) {
	var n int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM answer_cache`).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计缓存条数失败: %w", err)
	}
	return n, nil
}

// ShouldRefresh 判断命中缓存后是否要重新问一次模型。
//
// 用途是别让一个错误答案被永久缓存住：按概率挑一部分请求重新作答，
// 新答案会覆盖旧的。probability 为 0 表示永不刷新。
func ShouldRefresh(probability float64) bool {
	switch {
	case probability <= 0:
		return false
	case probability >= 1:
		return true
	default:
		return rand.Float64() < probability
	}
}
