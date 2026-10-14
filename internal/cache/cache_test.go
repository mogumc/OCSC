package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"ocs-ai-proxy/internal/model"
)

// openTemp 在临时目录里开一个缓存库。
func openTemp(t *testing.T) *Cache {
	t.Helper()

	c, err := Open(filepath.Join(t.TempDir(), "test_cache.db"))
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestPutAndGet(t *testing.T) {
	c := openTemp(t)
	req := &model.SearchRequest{Title: "1+1 等于几", Options: "A. 1\nB. 2", Type: model.TypeSingle}
	key := Key(req)

	// 未命中时应当是 (nil, nil)，而不是错误
	entry, err := c.Get(key)
	if err != nil {
		t.Fatalf("未命中不应报错: %v", err)
	}
	if entry != nil {
		t.Fatalf("未命中应返回 nil，实际 %+v", entry)
	}

	want := &Entry{Answer: "B", Analysis: "1+1=2"}
	if err := c.Put(key, req, want); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}

	got, err := c.Get(key)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got == nil || got.Answer != want.Answer || got.Analysis != want.Analysis {
		t.Fatalf("Get 结果 = %+v，期望 %+v", got, want)
	}
}

func TestPutOverwritesWithoutDuplicating(t *testing.T) {
	c := openTemp(t)
	req := &model.SearchRequest{Title: "重复题", Type: model.TypeSingle}
	key := Key(req)

	if err := c.Put(key, req, &Entry{Answer: "旧答案", Analysis: "旧解析"}); err != nil {
		t.Fatalf("首次 Put 失败: %v", err)
	}
	if err := c.Put(key, req, &Entry{Answer: "新答案", Analysis: "新解析"}); err != nil {
		t.Fatalf("二次 Put 失败: %v", err)
	}

	got, err := c.Get(key)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Answer != "新答案" || got.Analysis != "新解析" {
		t.Fatalf("同一道题再次写入应当覆盖，实际 %+v", got)
	}

	n, err := c.Count()
	if err != nil {
		t.Fatalf("Count 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("覆盖写入不应产生新行，Count = %d", n)
	}
}

func TestKeyDependsOnAllFields(t *testing.T) {
	base := &model.SearchRequest{Title: "题目", Options: "A\nB", Type: model.TypeSingle}
	baseKey := Key(base)

	t.Run("内容相同则键相同", func(t *testing.T) {
		same := &model.SearchRequest{Title: "题目", Options: "A\nB", Type: model.TypeSingle}
		if Key(same) != baseKey {
			t.Fatal("内容相同的请求应当得到相同的键")
		}
	})

	cases := map[string]*model.SearchRequest{
		"题型不同": {Title: "题目", Options: "A\nB", Type: model.TypeMultiple},
		"题干不同": {Title: "另一道题", Options: "A\nB", Type: model.TypeSingle},
		"选项不同": {Title: "题目", Options: "A\nC", Type: model.TypeSingle},
		"带图片":  {Title: "题目", Options: "A\nB", Type: model.TypeSingle, Images: []string{"data:image/png;base64,AAA"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if Key(req) == baseKey {
				t.Fatalf("%s 时应当算出不同的键，否则会串答案", name)
			}
		})
	}

	t.Run("图片不同则键不同", func(t *testing.T) {
		a := &model.SearchRequest{Title: "题目", Type: model.TypeSingle, Images: []string{"data:image/png;base64,AAA"}}
		b := &model.SearchRequest{Title: "题目", Type: model.TypeSingle, Images: []string{"data:image/png;base64,BBB"}}
		if Key(a) == Key(b) {
			t.Fatal("换了一张图应当算出不同的键")
		}
	})
}

func TestShouldRefresh(t *testing.T) {
	if ShouldRefresh(0) {
		t.Error("概率为 0 时不应刷新")
	}
	if ShouldRefresh(-0.5) {
		t.Error("概率为负数时不应刷新")
	}
	if !ShouldRefresh(1) {
		t.Error("概率为 1 时应当总是刷新")
	}
	if !ShouldRefresh(2) {
		t.Error("概率大于 1 时按总是刷新处理")
	}

	// 中间值应当是随机的：多跑几次必须两种结果都出现过
	const probability = 0.5
	var refreshed, kept int
	for i := 0; i < 2000; i++ {
		if ShouldRefresh(probability) {
			refreshed++
		} else {
			kept++
		}
	}
	if refreshed == 0 || kept == 0 {
		t.Fatalf("概率 %.1f 时应当两种结果都会出现，实际 刷新=%d 沿用=%d", probability, refreshed, kept)
	}
}

func TestOpenUsesWALAndCreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	path := filepath.Join(dir, "answer_cache.db")

	c, err := Open(path)
	if err != nil {
		t.Fatalf("Open 应当自动创建父目录，实际报错: %v", err)
	}
	defer c.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("缓存文件应当已创建: %v", err)
	}

	var mode string
	if err := c.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("读取 journal_mode 失败: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q，期望 wal（并发读写依赖它）", mode)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open("   "); err == nil {
		t.Fatal("空路径应当报错")
	}
}

func TestCloseIsNilSafe(t *testing.T) {
	var c *Cache
	if err := c.Close(); err != nil {
		t.Fatalf("nil 接收者调用 Close 应当安全返回: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := openTemp(t)

	// 并发读写不应触发 database is locked
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			req := &model.SearchRequest{Title: "并发题目", Type: model.TypeSingle}
			key := Key(req)
			for j := 0; j < 20; j++ {
				if err := c.Put(key, req, &Entry{Answer: "B", Analysis: "解析"}); err != nil {
					t.Errorf("并发 Put 失败: %v", err)
					return
				}
				if _, err := c.Get(key); err != nil {
					t.Errorf("并发 Get 失败: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	n, err := c.Count()
	if err != nil {
		t.Fatalf("Count 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("同一道题并发写入后应当只有 1 行，实际 %d", n)
	}
}
