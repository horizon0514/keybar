package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"keybar/agents"
	"keybar/vault"
)

// which are the agents named, or all those this system has.
func which(e *Env, names []string) (list []agents.Agent, named bool, err error) {
	if len(names) == 0 || len(names) == 1 && names[0] == "all" {
		return e.Agents.Supported(), false, nil
	}
	for _, n := range names {
		a, ok := agents.Find(n)
		if !ok {
			var ids []string
			for _, a := range e.Agents.Supported() {
				ids = append(ids, a.ID)
			}
			return nil, true, usageError("不认识的代理 " + n + "：" + strings.Join(ids, "、"))
		}
		if e.Agents.File(a) == "" {
			return nil, true, fmt.Errorf("%s 在这个系统上没有已知的设置文件", a.Name)
		}
		list = append(list, a)
	}
	return list, true, nil
}

func dot(s agents.State) string {
	switch s {
	case agents.Connected:
		return "●"
	case agents.Outdated, agents.NotConnected:
		return "○"
	}
	return " "
}

func (e *Env) statusLine(s agents.Status, note string) {
	where := ""
	if s.State != agents.NotInstalled {
		where = shortPath(s.File)
		if s.Via != "" {
			where += "（通过 " + s.Via + "）"
		}
	}
	if note == "" {
		note = s.State.Zh()
	}
	fmt.Fprintf(e.Stdout, "%s %-16s %-8s %s\n", dot(s.State), s.Name, note, where)
}

// missingLine lists the agents not installed, on one line.
func (e *Env) missingLine(names []string) {
	if len(names) > 0 {
		fmt.Fprintf(e.Stdout, "  未安装：%s\n", strings.Join(names, "、"))
	}
}

func connect(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	status := fs.Bool("status", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if e.Agents == nil {
		return 1, errors.New("这里不能连接代理")
	}
	list, named, err := which(e, pos)
	if err != nil {
		return 2, err
	}
	var missing []string
	if *status {
		for _, a := range list {
			s := e.Agents.StatusOf(a)
			if s.State == agents.NotInstalled && !named {
				missing = append(missing, a.Name)
				continue
			}
			e.statusLine(s, "")
		}
		e.missingLine(missing)
		return 0, nil
	}
	if err := e.Agents.Check(); err != nil {
		return 1, err
	}
	connected, failed := 0, 0
	for _, a := range list {
		s := e.Agents.StatusOf(a)
		if s.State == agents.NotInstalled && !named {
			missing = append(missing, a.Name)
			continue
		}
		was := s.State
		s, err := e.Agents.Connect(a)
		switch {
		case err != nil:
			failed++
			e.statusLine(s, "失败")
			fmt.Fprintf(e.Stderr, "  %s：%v\n", a.Name, err)
		case was == agents.Connected:
			connected++
			e.statusLine(s, "已连接")
		default:
			connected++
			e.statusLine(s, "已连接")
			_ = e.Vault.Log(vault.Event{Client: "cli", Action: "connect", Decision: "ok", Detail: a.Name})
		}
	}
	e.missingLine(missing)
	switch {
	case connected == 0 && failed == 0:
		fmt.Fprintln(e.Stdout, "没找到支持的 AI 代理。装好后再运行 keys connect（列表见 keys connect --status）。")
		return 1, nil
	case connected > 0:
		fmt.Fprintln(e.Stdout, "重启代理（或开个新会话）后生效：它会看到 KeyBar 的 run_with_secrets 工具，每次用密钥都要你批准。")
	}
	if failed > 0 {
		return 1, nil
	}
	return 0, nil
}

func disconnect(e *Env, args []string) (int, error) {
	if e.Agents == nil {
		return 1, errors.New("这里不能断开代理")
	}
	list, named, err := which(e, args)
	if err != nil {
		return 2, err
	}
	code := 0
	for _, a := range list {
		was := e.Agents.StatusOf(a).State
		if was == agents.NotInstalled && !named {
			continue
		}
		s, err := e.Agents.Disconnect(a)
		if err != nil {
			code = 1
			e.statusLine(s, "失败")
			fmt.Fprintf(e.Stderr, "  %s：%v\n", a.Name, err)
			continue
		}
		e.statusLine(s, "")
		if was == agents.Connected || was == agents.Outdated {
			_ = e.Vault.Log(vault.Event{Client: "cli", Action: "disconnect", Decision: "ok", Detail: a.Name})
		}
	}
	return code, nil
}
