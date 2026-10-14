package handler

import "testing"

func TestCleanOptions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"去掉空行与行首尾空白", "A. 1\n\n   B. 2  \nC. 3", "A. 1\nB. 2\nC. 3"},
		{"全是空白时得到空串", "  \n\t\n ", ""},
		{"空输入得到空串", "", ""},
		{"单行原样保留", "A. 1", "A. 1"},
		{"保留选项内部空格", "A. 新  中国\nB. 其他", "A. 新  中国\nB. 其他"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanOptions(c.in); got != c.want {
				t.Fatalf("cleanOptions(%q) = %q，期望 %q", c.in, got, c.want)
			}
		})
	}
}
