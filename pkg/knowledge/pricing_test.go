package knowledge

import "testing"

func TestKnowledgeBailianBaseValidation(t *testing.T) {
	for _, raw := range []string{"https://dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://workspace.cn-beijing.maas.aliyuncs.com/"} {
		if _, err := AliBatchBase(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"http://dashscope.aliyuncs.com", "https://dashscope-intl.aliyuncs.com", "https://dashscope.aliyuncs.com.evil.test", "https://dashscope.aliyuncs.com:443", "https://user@dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com/custom"} {
		if _, err := AliBatchBase(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
