package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckLLMRoute(t *testing.T) {
	for _, test := range []struct {
		name      string
		object    string
		effective string
		wantErr   bool
	}{
		{name: "相同路由通过", object: LLMRouteServer, effective: LLMRouteServer},
		{name: "客户端路由通过", object: LLMRouteClient, effective: LLMRouteClient},
		{name: "对象与设置不一致", object: LLMRouteServer, effective: LLMRouteClient, wantErr: true},
		{name: "设置切回后旧对话不可用", object: LLMRouteClient, effective: LLMRouteServer, wantErr: true},
		{name: "空对象路由视为 server", object: "", effective: LLMRouteServer},
		{name: "空有效路由视为 server", object: LLMRouteServer, effective: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := CheckLLMRoute(test.object, test.effective)
			if test.wantErr && !errors.Is(err, ErrLLMRouteMismatch) {
				t.Fatalf("CheckLLMRoute(%q, %q) error = %v, want ErrLLMRouteMismatch", test.object, test.effective, err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("CheckLLMRoute(%q, %q) error = %v, want nil", test.object, test.effective, err)
			}
		})
	}
}

func TestLLMRouteMismatchMessageNamesBothRoutes(t *testing.T) {
	message := LLMRouteMismatchMessage(LLMRouteClient, LLMRouteServer)
	if !strings.Contains(message, "客户端直连") || !strings.Contains(message, "服务端转发") {
		t.Fatalf("mismatch message does not name both routes: %q", message)
	}
}
