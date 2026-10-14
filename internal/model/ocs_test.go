package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImagesUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "数组（OCS 推荐写法）",
			in:   `["data:image/png;base64,AAA","data:image/png;base64,BBB"]`,
			want: []string{"data:image/png;base64,AAA", "data:image/png;base64,BBB"},
		},
		{
			name: "单元素数组",
			in:   `["data:image/png;base64,AAA"]`,
			want: []string{"data:image/png;base64,AAA"},
		},
		{
			name: "空数组",
			in:   `[]`,
			want: nil,
		},
		{
			name: "字符串单张（配置写成 ${images} 时的形态）",
			in:   `"data:image/png;base64,AAA"`,
			want: []string{"data:image/png;base64,AAA"},
		},
		{
			name: "字符串多张按换行拼接",
			in:   `"data:image/png;base64,AAA\ndata:image/png;base64,BBB\n\n"`,
			want: []string{"data:image/png;base64,AAA", "data:image/png;base64,BBB"},
		},
		{
			name: "空字符串",
			in:   `""`,
			want: nil,
		},
		{
			name: "null",
			in:   `null`,
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got Images
			if err := json.Unmarshal([]byte(c.in), &got); err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			// 用拼接比较，避免 nil 与空切片的差异干扰
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("解析结果 = %v，期望 %v", []string(got), c.want)
			}
		})
	}
}

func TestImagesUnmarshalRejectsOtherTypes(t *testing.T) {
	// 数字、对象等真正不合法的类型仍应报错，不能悄悄吞掉
	for _, in := range []string{`123`, `{"a":1}`} {
		var got Images
		if err := json.Unmarshal([]byte(in), &got); err == nil {
			t.Fatalf("%s 应当报错", in)
		}
	}
}

func TestSearchRequestWithStringImages(t *testing.T) {
	// 端到端：配置写错成 "${images}" 时，整个请求不能因此 400，
	// 否则连纯文字题都会一起失败
	body := `{"title":"题目","options":"A. 1","type":"single","images":"data:image/png;base64,AAA"}`

	var req SearchRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("请求体不应解析失败: %v", err)
	}
	if len(req.Images) != 1 || req.Images[0] != "data:image/png;base64,AAA" {
		t.Fatalf("images 解析结果 = %v", []string(req.Images))
	}
}

func TestSearchRequestIgnoresUnknownFields(t *testing.T) {
	// OCS 图片题优化会附带 suggestion_* 等字段，未识别的字段不应导致失败
	body := `{"title":"题目","type":"single","suggestion_title":"建议题干","未来字段":123}`

	var req SearchRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("未知字段不应导致解析失败: %v", err)
	}
	if req.SuggestionTitle != "建议题干" {
		t.Fatalf("suggestion_title = %q", req.SuggestionTitle)
	}
}
