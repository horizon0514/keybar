// Package cli is the keys command: KeyBar from a terminal.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"keybar/approval"
	"keybar/mcp"
	"keybar/project"
	"keybar/redact"
	"keybar/remote"
	"keybar/runner"
	"keybar/vault"
)

// Env is what a command runs with: tests give it a vault in memory, pipes
// and a scripted terminal.
type Env struct {
	Vault   *vault.Vault
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	// Terminal tells whether stdin is a terminal; Password reads a line
	// from it without echo.
	Terminal func() bool
	Password func(prompt string) ([]byte, error)
	Gate     interface {
		Ask(approval.Request) approval.Decision
	}
	Syncer *remote.Syncer
	Getwd  func() (string, error)
	// Exec runs a program for keys run; it returns the exit code.
	Exec func(cmd *exec.Cmd) (int, error)
}

type usageError string

func (e usageError) Error() string { return string(e) }

// Main runs args (without the program's name) and returns the exit code.
func Main(e *Env, args []string) int {
	if len(args) == 0 {
		args = []string{"help"}
	}
	cmd, rest := args[0], args[1:]
	run, ok := commands()[cmd]
	if !ok {
		fmt.Fprintf(e.Stderr, "keys：没有命令 %q。keys help 看用法。\n", cmd)
		return 2
	}
	code, err := run(e, rest)
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(e.Stderr, "keys %s：%v\n", cmd, err)
			return 2
		}
		fmt.Fprintf(e.Stderr, "keys %s：%v\n", cmd, err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

type command func(e *Env, args []string) (int, error)

func commands() map[string]command {
	return map[string]command{
		"help": help, "-h": help, "--help": help,
		"version": func(e *Env, _ []string) (int, error) {
			fmt.Fprintln(e.Stdout, "keys (KeyBar) "+e.Version)
			return 0, nil
		},
		"add": add, "set": set, "get": get, "ls": ls, "list": ls, "show": show, "rm": rm, "edit": edit,
		"run": runCmd, "import": importCmd, "rotate": rotate, "done": done, "undone": undone, "sync": syncCmd,
		"audit": audit, "doctor": doctor, "mcp": mcpCmd, "config": config,
		"export": func(e *Env, _ []string) (int, error) {
			return 2, errors.New("没有 export：KeyBar 不把密钥批量写成明文文件。用 keys run -- <命令> 注入到程序，或 keys get NAME --reveal 读一个")
		},
	}
}

const usage = `keys — KeyBar 的命令行：密钥只存在系统钥匙串里，程序和 AI 代理按需取用。

用法：
  keys add NAME [选项]        添加密钥（值从标准输入或隐藏提示读，绝不从参数读）
  keys set NAME               改值（已有的密钥视为一次轮换）
  keys get NAME --reveal      输出明文（会记日志；非交互时需要批准）
  keys ls [--json] [-q 关键词] 列出密钥（只有元数据，没有值）
  keys show NAME              详情：平台清单、轮换历史
  keys edit NAME [选项]       改元数据
  keys rm NAME [-y]           删除
  keys run [-p keys.toml] [-e VAR=KEY]... [--redact] -- 命令 参数...
                              只把需要的密钥注入子进程的环境变量
  keys import .env [--prefix P] [--project 名] [--shred]
                              导入 .env，生成 keys.toml，可选覆盖并删除 .env
  keys rotate NAME [--no-sync] [--note 说明]
                              换新值，自动更新 GitHub Actions / Vercel，列出其余要手动更新的平台
  keys done NAME ID|all       勾掉清单上已更新的平台（undone 撤销）
  keys sync NAME [ID]         重试自动更新
  keys audit [-n 50] [--key NAME]  访问记录
  keys doctor [目录...]        检查超期 / 快到期的密钥、未完成的清单、明文 .env
  keys config [名 值]          设置：rotate-days、remind、remind-days、grant-minutes、cli-approval
  keys mcp [--client 名]      MCP 服务（stdio），给 Claude Code、Cursor 等代理用

add / edit 的选项：
  --desc 说明   --tag 标签   --project 项目   --rotate-days N（-1 不提醒）
  --expires 2027-01-19（none 清除）   --agent ask|deny（deny：代理一律不能用）
  --platform kind:target[:VAR][?env=a,b&team=T&token=KEY]
      如 github:owner/repo、vercel:my-app?env=production、railway:api、env:~/proj/.env
  edit 另有：--untag、--unproject、--rm-platform ID

没有 export：KeyBar 不把密钥批量写成明文文件。
`

func help(e *Env, _ []string) (int, error) {
	fmt.Fprint(e.Stdout, usage)
	return 0, nil
}

// multi is a repeatable flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

// parse parses flags anywhere among the positional arguments, up to "--".
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos, tail []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, tail = args[:i], args[i:]
	}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageError(err.Error())
		}
		args = fs.Args()
		if len(args) == 0 {
			return append(pos, tail...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (e *Env) now() time.Time {
	if e.Vault.Now != nil {
		return e.Vault.Now()
	}
	return time.Now()
}

func (e *Env) terminal() bool { return e.Terminal != nil && e.Terminal() }

// readValue reads a value: hidden, twice, from a terminal; else all of
// stdin, without its last newline.
func (e *Env) readValue(name string, confirm bool) ([]byte, error) {
	if e.terminal() && e.Password != nil {
		v, err := e.Password(name + " 的值（输入不显示）：")
		if err != nil {
			return nil, err
		}
		if confirm {
			again, err := e.Password("再输入一次：")
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(v, again) {
				return nil, errors.New("两次输入不一致")
			}
		}
		return v, nil
	}
	b, err := io.ReadAll(io.LimitReader(e.Stdin, vault.MaxValueSize+2))
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSuffix(b, []byte("\n"))
	b = bytes.TrimSuffix(b, []byte("\r"))
	if len(b) == 0 {
		return nil, errors.New("没有读到值：从管道传入（printf %s \"$V\" | keys add NAME），或在终端里运行以隐藏输入")
	}
	return b, nil
}

// ask asks a yes/no question on a terminal; without one, the answer is def.
func (e *Env) ask(q string, def bool) bool {
	if !e.terminal() {
		return def
	}
	hint := " [y/N] "
	if def {
		hint = " [Y/n] "
	}
	fmt.Fprint(e.Stderr, q+hint)
	line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "是":
		return true
	case "n", "no", "否":
		return false
	}
	return def
}

type keyFlags struct {
	desc, expires, agent  *string
	rotate                *int
	tags, projects, plats multi
}

func addKeyFlags(fs *flag.FlagSet) *keyFlags {
	f := &keyFlags{desc: fs.String("desc", "", ""), expires: fs.String("expires", "", ""), agent: fs.String("agent", "", ""), rotate: fs.Int("rotate-days", 0, "")}
	fs.Var(&f.tags, "tag", "")
	fs.Var(&f.projects, "project", "")
	fs.Var(&f.plats, "platform", "")
	return f
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{time.DateOnly, time.RFC3339, "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, usageError("日期格式是 2027-01-19：" + s)
}

func (f *keyFlags) apply(k *vault.Key, set map[string]bool, now time.Time) error {
	if set["desc"] {
		k.Description = *f.desc
	}
	if set["rotate-days"] {
		k.RotateDays = *f.rotate
	}
	if set["expires"] {
		if *f.expires == "none" || *f.expires == "" {
			k.Expires = time.Time{}
		} else {
			t, err := parseDate(*f.expires)
			if err != nil {
				return err
			}
			k.Expires = t
		}
	}
	if set["agent"] {
		switch *f.agent {
		case "ask", "deny":
			k.Agent = *f.agent
			if k.Agent == "ask" {
				k.Agent = ""
			}
		default:
			return usageError("--agent 是 ask 或 deny")
		}
	}
	k.Tags = vault.Clean(append(k.Tags, f.tags...))
	k.Projects = vault.Clean(append(k.Projects, f.projects...))
	for _, s := range f.plats {
		p, err := vault.ParsePlatform(s)
		if err != nil {
			return usageError(err.Error())
		}
		if k.Name == "" || k.Created.IsZero() {
			k.Platforms = append(k.Platforms, p)
		} else {
			k.AddPlatform(p, now)
		}
	}
	return nil
}

func setFlags(fs *flag.FlagSet) map[string]bool {
	m := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { m[f.Name] = true })
	return m
}

func add(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	kf := addKeyFlags(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) != 1 {
		return 2, usageError("用法：keys add NAME [--desc 说明] [--platform github:owner/repo]…（值从标准输入或提示读）")
	}
	name := pos[0]
	if err := vault.CheckName(name); err != nil {
		return 2, err
	}
	if inv, err := e.Vault.Inventory(); err == nil {
		if _, ok := inv.Find(name); ok {
			return 1, fmt.Errorf("密钥 %s 已存在；换新值请用 keys rotate %s", name, name)
		}
	}
	k := vault.Key{Name: name}
	if err := kf.apply(&k, setFlags(fs), e.now()); err != nil {
		return 2, err
	}
	v, err := e.readValue(name, true)
	if err != nil {
		return 1, err
	}
	if err := e.Vault.Add(k, v); err != nil {
		return 1, err
	}
	_ = e.Vault.Log(vault.Event{Client: "cli", Action: "add", Keys: []string{name}, Decision: "ok"})
	fmt.Fprintf(e.Stdout, "已添加 %s（值在%s里）\n", name, storeName())
	return 0, nil
}

func storeName() string {
	switch goos {
	case "darwin":
		return " macOS 钥匙串"
	case "windows":
		return " Windows 凭据管理器"
	}
	return "系统密钥环"
}

func set(e *Env, args []string) (int, error) {
	if len(args) != 1 {
		return 2, usageError("用法：keys set NAME（值从标准输入或提示读）")
	}
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	if _, ok := inv.Find(args[0]); !ok {
		return add(e, args)
	}
	return rotate(e, args)
}

// approveCLI asks for approval when the command line is used without a
// terminal, as an agent's shell does.
func (e *Env) approveCLI(action string, keys []string, command string, plain bool) (asked bool, err error) {
	if e.terminal() || !vault.LoadSettings(e.Vault.Dir).CLIApproval {
		return false, nil
	}
	if e.Gate == nil {
		return true, errors.New("非交互使用需要批准，但没有可用的批准方式")
	}
	cwd := ""
	if e.Getwd != nil {
		cwd, _ = e.Getwd()
	}
	d := e.Gate.Ask(approval.Request{Client: "cli", Action: action, Keys: keys, Command: command, Cwd: cwd, PID: os.Getpid(), Plain: plain})
	_ = e.Vault.Log(vault.Event{Client: "cli", Action: action, Keys: keys, Decision: d.Code(), Detail: command})
	if !d.Allow {
		return true, fmt.Errorf("没有批准（非交互使用需要在 KeyBar 里批准；%s）", vault.DecisionZh(d.Code()))
	}
	return true, nil
}

func get(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	reveal := fs.Bool("reveal", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) != 1 {
		return 2, usageError("用法：keys get NAME --reveal")
	}
	name := pos[0]
	if !*reveal {
		return 2, usageError("读明文要加 --reveal。更安全的做法：keys run -- <命令>，值只进子进程的环境变量")
	}
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	if _, ok := inv.Find(name); !ok {
		return 1, fmt.Errorf("没有密钥 %s", name)
	}
	asked, err := e.approveCLI("reveal", []string{name}, "", false)
	if err != nil {
		return 1, err
	}
	v, err := e.Vault.Value(name)
	if err != nil {
		return 1, err
	}
	if !asked {
		_ = e.Vault.Log(vault.Event{Client: "cli", Action: "reveal", Keys: []string{name}, Decision: "ok"})
	}
	fmt.Fprintln(e.Stderr, "注意：明文已输出，别粘贴到聊天、日志或代码里。")
	_, _ = e.Stdout.Write(v)
	if f, ok := e.Stdout.(*os.File); ok && isTerminal(f) {
		fmt.Fprintln(e.Stdout)
	}
	return 0, nil
}

func ls(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	q := fs.String("q", "", "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) > 0 && *q == "" {
		*q = pos[0]
	}
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	now := e.now()
	var keys []vault.Key
	for _, k := range inv.Keys {
		if matches(k, *q) {
			keys = append(keys, k)
		}
	}
	if *asJSON {
		return 0, writeJSON(e.Stdout, inv, keys, now)
	}
	if len(keys) == 0 {
		if len(inv.Keys) == 0 {
			fmt.Fprintln(e.Stdout, "还没有密钥。keys add NAME 添加，或 keys import .env 导入。")
		} else {
			fmt.Fprintln(e.Stdout, "没有匹配的密钥。")
		}
		return 0, nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "名称\t状态\t上次轮换\t平台\t说明")
	for _, k := range keys {
		st := inv.StatusOf(k, now)
		state := st.State.Zh()
		if st.Why != "" {
			state += "（" + st.Why + "）"
		}
		plats := fmt.Sprint(len(k.Platforms))
		if st.Pending > 0 {
			plats += fmt.Sprintf("（%d 待更新）", st.Pending)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k.Name, state, vault.Ago(k.LastRotated, now), plats, k.Description)
	}
	return 0, tw.Flush()
}

func matches(k vault.Key, q string) bool {
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join(append(append([]string{k.Name, k.Description}, k.Tags...), k.Projects...), " "))
	for _, p := range k.Platforms {
		hay += " " + strings.ToLower(p.Label())
	}
	return strings.Contains(hay, strings.ToLower(q))
}

func show(e *Env, args []string) (int, error) {
	if len(args) != 1 {
		return 2, usageError("用法：keys show NAME")
	}
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	k, ok := inv.Find(args[0])
	if !ok {
		return 1, fmt.Errorf("没有密钥 %s", args[0])
	}
	now := e.now()
	st := inv.StatusOf(*k, now)
	w := e.Stdout
	fmt.Fprintf(w, "%s\n", k.Name)
	if k.Description != "" {
		fmt.Fprintf(w, "  说明      %s\n", k.Description)
	}
	state := st.State.Zh()
	if st.Why != "" {
		state += "（" + st.Why + "）"
	}
	fmt.Fprintf(w, "  状态      %s\n", state)
	fmt.Fprintf(w, "  创建      %s\n", day(k.Created))
	fmt.Fprintf(w, "  上次轮换  %s（%s）\n", day(k.LastRotated), vault.Ago(k.LastRotated, now))
	if !st.Due.IsZero() {
		fmt.Fprintf(w, "  下次轮换  %s（每 %d 天）\n", day(st.Due), int(inv.RotateEvery(*k).Hours()/24))
	} else {
		fmt.Fprintf(w, "  下次轮换  不提醒\n")
	}
	if !k.Expires.IsZero() {
		fmt.Fprintf(w, "  过期      %s\n", day(k.Expires))
	}
	if len(k.Tags) > 0 {
		fmt.Fprintf(w, "  标签      %s\n", strings.Join(k.Tags, ", "))
	}
	if len(k.Projects) > 0 {
		fmt.Fprintf(w, "  使用者    %s\n", strings.Join(k.Projects, ", "))
	}
	agent := "每次需要批准"
	if k.Agent == "deny" {
		agent = "不允许"
	}
	fmt.Fprintf(w, "  AI 代理   %s\n", agent)
	fmt.Fprintf(w, "  也存在于（%d）\n", len(k.Platforms))
	for _, p := range k.Platforms {
		fmt.Fprintf(w, "    %s\n", platformLine(*k, p))
	}
	if len(k.Platforms) == 0 {
		fmt.Fprintln(w, "    —  用 keys edit NAME --platform github:owner/repo 记下它还存在哪里")
	}
	if len(k.History) > 0 {
		fmt.Fprintln(w, "  轮换历史")
		for i := len(k.History) - 1; i >= 0 && i >= len(k.History)-10; i-- {
			h := k.History[i]
			line := "    " + h.At.Local().Format("2006-01-02 15:04")
			if h.By != "" {
				line += "  " + h.By
			}
			if h.Note != "" {
				line += "  " + h.Note
			}
			fmt.Fprintln(w, line)
		}
	}
	return 0, nil
}

func platformLine(k vault.Key, p vault.Platform) string {
	box := "[x]"
	if p.Stale(k) {
		box = "[ ]"
	}
	s := fmt.Sprintf("%s %s  %s", box, p.ID, p.Label())
	if v := p.VarName(k.Name); v != k.Name {
		s += "  变量 " + v
	}
	if len(p.Envs) > 0 {
		s += "  环境 " + strings.Join(p.Envs, ",")
	}
	if p.Auto() {
		s += "  （可自动更新）"
	}
	if p.Note != "" {
		s += "  " + p.Note
	}
	return s
}

func day(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format(time.DateOnly)
}

func rm(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	yes := fs.Bool("y", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) != 1 {
		return 2, usageError("用法：keys rm NAME [-y]")
	}
	name := pos[0]
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	k, ok := inv.Find(name)
	if !ok {
		return 1, fmt.Errorf("没有密钥 %s", name)
	}
	if !*yes {
		if !e.terminal() {
			return 2, usageError("删除要确认：加 -y")
		}
		if len(k.Platforms) > 0 {
			fmt.Fprintf(e.Stderr, "%s 还存在于 %d 个平台，删除后 KeyBar 不再提醒你轮换它们。\n", name, len(k.Platforms))
		}
		if !e.ask("删除 "+name+"（钥匙串里的值也删除）？", false) {
			return 1, errors.New("已取消")
		}
	}
	if err := e.Vault.Remove(name); err != nil {
		return 1, err
	}
	_ = e.Vault.Log(vault.Event{Client: "cli", Action: "rm", Keys: []string{name}, Decision: "ok"})
	fmt.Fprintf(e.Stdout, "已删除 %s\n", name)
	return 0, nil
}

func edit(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	kf := addKeyFlags(fs)
	var untag, unproject, rmPlat multi
	fs.Var(&untag, "untag", "")
	fs.Var(&unproject, "unproject", "")
	fs.Var(&rmPlat, "rm-platform", "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) != 1 {
		return 2, usageError("用法：keys edit NAME [--desc …] [--platform kind:target] [--rm-platform ID] …")
	}
	set := setFlags(fs)
	if len(set) == 0 {
		return 2, usageError("没有要改的：keys help 看 edit 的选项")
	}
	now := e.now()
	var out vault.Key
	err = e.Vault.Edit(pos[0], func(k *vault.Key) error {
		if err := kf.apply(k, set, now); err != nil {
			return err
		}
		k.Tags = slices.DeleteFunc(k.Tags, func(t string) bool { return slices.Contains(untag, t) })
		k.Projects = slices.DeleteFunc(k.Projects, func(t string) bool { return slices.Contains(unproject, t) })
		for _, id := range rmPlat {
			n := len(k.Platforms)
			k.Platforms = slices.DeleteFunc(k.Platforms, func(p vault.Platform) bool { return p.ID == id })
			if len(k.Platforms) == n {
				return fmt.Errorf("没有平台 %s", id)
			}
		}
		out = *k
		return nil
	})
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			return 2, err
		}
		return 1, err
	}
	fmt.Fprintf(e.Stdout, "已更新 %s\n", out.Name)
	return 0, nil
}

func runCmd(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	proj := fs.String("p", "", "")
	fs.StringVar(proj, "project", "", "")
	var envs multi
	fs.Var(&envs, "e", "")
	redactOut := fs.Bool("redact", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	i := slices.Index(pos, "--")
	if i < 0 || i != 0 || len(pos) < 2 {
		return 2, usageError("用法：keys run [-p keys.toml] [-e VAR=KEY]... [--redact] -- 命令 参数...")
	}
	argv := pos[1:]
	env := map[string]string{}
	for _, kv := range envs {
		v, k, ok := strings.Cut(kv, "=")
		if !ok {
			v, k = kv, kv
		}
		env[v] = k
	}
	if *proj != "" || len(env) == 0 {
		path := *proj
		if path == "" {
			wd, _ := e.Getwd()
			p, ok := project.Find(wd)
			if !ok {
				return 2, usageError("没找到 keys.toml：用 -p 指定，或 -e VAR=KEY 列出要注入的密钥")
			}
			path = p
		}
		f, err := project.Load(path)
		if err != nil {
			return 1, err
		}
		for v, k := range f.Env {
			if _, ok := env[v]; !ok {
				env[v] = k
			}
		}
	}
	var names []string
	for _, k := range env {
		if !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	var missing []string
	for _, n := range names {
		if _, ok := inv.Find(n); !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return 1, fmt.Errorf("KeyBar 里没有：%s（keys add 添加）", strings.Join(missing, ", "))
	}
	line := runner.Display(argv)
	asked, err := e.approveCLI("run", names, line, !*redactOut)
	if err != nil {
		return 1, err
	}
	values, err := e.Vault.Values(names)
	if err != nil {
		return 1, err
	}
	inject := map[string][]byte{}
	for v, k := range env {
		inject[v] = values[k]
	}
	if !asked {
		_ = e.Vault.Log(vault.Event{Client: "cli", Action: "run", Keys: names, Decision: "ok", Detail: filepath.Base(argv[0])})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	wd, _ := e.Getwd()
	cmd, err := runner.Command(ctx, argv, inject, wd)
	if err != nil {
		return 2, err
	}
	cmd.Stdin = e.Stdin
	cmd.Stdout, cmd.Stderr = e.Stdout, e.Stderr
	var closers []io.Closer
	if *redactOut {
		r := redact.New(values) // labeled by key name
		o, er := r.Writer(e.Stdout), r.Writer(e.Stderr)
		cmd.Stdout, cmd.Stderr = o, er
		closers = append(closers, o, er)
	}
	code, err := e.Exec(cmd)
	for _, c := range closers {
		_ = c.Close()
	}
	return code, err
}

// ExecCmd runs cmd and returns its exit code.
func ExecCmd(cmd *exec.Cmd) (int, error) {
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 127, err
	}
	return 0, nil
}

func importCmd(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	prefix := fs.String("prefix", "", "")
	proj := fs.String("project", "", "")
	shred := fs.Bool("shred", false, "")
	keep := fs.Bool("keep", false, "")
	noToml := fs.Bool("no-toml", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) != 1 {
		return 2, usageError("用法：keys import .env [--prefix P] [--project 名] [--shred | --keep] [--no-toml]")
	}
	path, _ := filepath.Abs(pos[0])
	b, err := os.ReadFile(path)
	if err != nil {
		return 1, err
	}
	vars, err := project.ParseDotenv(b)
	if err != nil {
		return 1, fmt.Errorf("%s：%w", path, err)
	}
	if len(vars) == 0 {
		return 1, errors.New("文件里没有变量")
	}
	dir := filepath.Dir(path)
	name := *proj
	if name == "" {
		name = filepath.Base(dir)
	}
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	env := map[string]string{}
	added, skipped := 0, 0
	for _, v := range vars {
		key := *prefix + v.Name
		if strings.TrimSpace(v.Value) == "" {
			fmt.Fprintf(e.Stderr, "  跳过 %s：值是空的\n", v.Name)
			skipped++
			continue
		}
		if _, ok := inv.Find(key); ok {
			old, err := e.Vault.Value(key)
			if err == nil && bytes.Equal(old, []byte(v.Value)) {
				fmt.Fprintf(e.Stderr, "  %s 已在 KeyBar 里（值相同），只记下项目\n", key)
				_ = e.Vault.Edit(key, func(k *vault.Key) error { k.Projects = vault.Clean(append(k.Projects, name)); return nil })
				env[v.Name] = key
				continue
			}
			fmt.Fprintf(e.Stderr, "  跳过 %s：KeyBar 里已有同名密钥但值不同（用 --prefix 换个名字导入）\n", key)
			skipped++
			continue
		}
		k := vault.Key{Name: key, Projects: []string{name}, Description: "从 " + shortPath(path) + " 导入"}
		if err := e.Vault.Add(k, []byte(v.Value)); err != nil {
			fmt.Fprintf(e.Stderr, "  %s：%v\n", key, err)
			skipped++
			continue
		}
		env[v.Name] = key
		added++
		fmt.Fprintf(e.Stdout, "  + %s\n", key)
	}
	_ = e.Vault.Log(vault.Event{Client: "cli", Action: "import", Keys: slices.Sorted(mapValues(env)), Decision: "ok", Detail: shortPath(path)})
	fmt.Fprintf(e.Stdout, "导入 %d 个，跳过 %d 个。\n", added, skipped)
	if len(env) > 0 && !*noToml {
		tomlPath := filepath.Join(dir, project.FileName)
		if old, err := project.Load(tomlPath); err == nil {
			for k, v := range old.Env {
				if _, ok := env[k]; !ok {
					env[k] = v
				}
			}
		}
		if err := project.Write(tomlPath, name, env); err != nil {
			return 1, err
		}
		fmt.Fprintf(e.Stdout, "已写 %s（只有名字，没有值，可以提交）。之后用：keys run -- <命令>\n", shortPath(tomlPath))
	}
	if skipped > 0 && (*shred || !*keep) {
		fmt.Fprintf(e.Stderr, "有 %d 个没导入，保留 %s。\n", skipped, shortPath(path))
		return 0, nil
	}
	switch {
	case *keep:
	case *shred || e.ask("覆盖并删除 "+shortPath(path)+"？（值已在钥匙串里）", false):
		if err := project.Shred(path); err != nil {
			return 1, fmt.Errorf("删除 %s：%w", path, err)
		}
		fmt.Fprintf(e.Stdout, "已覆盖并删除 %s。如果它曾提交到 git 或被备份过，请轮换这些密钥。\n", shortPath(path))
	default:
		fmt.Fprintf(e.Stdout, "保留了 %s：确认程序能用 keys run 跑起来后，记得删除它（keys import 时加 --shred）。\n", shortPath(path))
	}
	return 0, nil
}

func mapValues(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		seen := map[string]bool{}
		for _, v := range m {
			if !seen[v] {
				seen[v] = true
				if !yield(v) {
					return
				}
			}
		}
	}
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func rotate(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("rotate", flag.ContinueOnError)
	noSync := fs.Bool("no-sync", false, "")
	note := fs.String("note", "", "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) != 1 {
		return 2, usageError("用法：keys rotate NAME [--no-sync] [--note 说明]")
	}
	name := pos[0]
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	k, ok := inv.Find(name)
	if !ok {
		return 1, fmt.Errorf("没有密钥 %s", name)
	}
	if e.terminal() {
		fmt.Fprintf(e.Stderr, "轮换 %s：先在服务商那里生成新密钥，再把新值粘贴进来。\n", name)
		if len(k.Platforms) > 0 {
			fmt.Fprintf(e.Stderr, "之后要更新的平台：%d 个。\n", len(k.Platforms))
		}
	}
	v, err := e.readValue(name+" 的新值", true)
	if err != nil {
		return 1, err
	}
	e.Vault.By = "cli"
	nk, err := e.Vault.Rotate(name, v, *note)
	if err != nil {
		return 1, err
	}
	_ = e.Vault.Log(vault.Event{Client: "cli", Action: "rotate", Keys: []string{name}, Decision: "ok"})
	fmt.Fprintf(e.Stdout, "已换新 %s。\n", name)
	if !*noSync {
		e.syncAll(nk, v, nil)
	}
	return 0, e.checklist(name)
}

// syncAll updates the key's automatic platforms (those of ids, or all).
func (e *Env) syncAll(k vault.Key, value []byte, ids []string) {
	for _, p := range k.Platforms {
		if !p.Auto() || (len(ids) > 0 && !slices.Contains(ids, p.ID)) || (len(ids) == 0 && !p.Stale(k)) {
			continue
		}
		if e.Syncer == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		err := e.Syncer.Update(ctx, k.Name, p, value)
		cancel()
		ev := vault.Event{Client: "cli", Action: "sync", Keys: []string{k.Name}, Detail: p.Label(), Decision: "ok"}
		if err != nil {
			ev.Decision = "error"
			fmt.Fprintf(e.Stdout, "  ✗ %s：%v\n", p.Label(), err)
		} else {
			_ = e.Vault.MarkUpdated(k.Name, p.ID)
			fmt.Fprintf(e.Stdout, "  ✓ %s 已自动更新\n", p.Label())
		}
		_ = e.Vault.Log(ev)
	}
}

// checklist prints what is left to update.
func (e *Env) checklist(name string) error {
	inv, err := e.Vault.Inventory()
	if err != nil {
		return err
	}
	k, _ := inv.Find(name)
	pending := k.Pending()
	if len(pending) == 0 {
		if len(k.Platforms) > 0 {
			fmt.Fprintln(e.Stdout, "所有平台都已更新。")
		}
		fmt.Fprintln(e.Stdout, "最后：到服务商那里吊销旧密钥。")
		return nil
	}
	fmt.Fprintf(e.Stdout, "还要手动更新（%d）：\n", len(pending))
	for _, p := range pending {
		fmt.Fprintf(e.Stdout, "  %s\n", platformLine(*k, p))
	}
	fmt.Fprintf(e.Stdout, "更新一个就勾一个：keys done %s <ID>（全部：keys done %s all）。最后到服务商那里吊销旧密钥。\n", name, name)
	return nil
}

func done(e *Env, args []string) (int, error) {
	if len(args) != 2 {
		return 2, usageError("用法：keys done NAME ID|all")
	}
	if err := e.Vault.MarkUpdated(args[0], args[1]); err != nil {
		return 1, err
	}
	return 0, e.checklist(args[0])
}

func undone(e *Env, args []string) (int, error) {
	if len(args) != 2 {
		return 2, usageError("用法：keys undone NAME ID")
	}
	if err := e.Vault.Unmark(args[0], args[1]); err != nil {
		return 1, err
	}
	return 0, e.checklist(args[0])
}

func syncCmd(e *Env, args []string) (int, error) {
	if len(args) < 1 {
		return 2, usageError("用法：keys sync NAME [ID...]")
	}
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	k, ok := inv.Find(args[0])
	if !ok {
		return 1, fmt.Errorf("没有密钥 %s", args[0])
	}
	v, err := e.Vault.Value(k.Name)
	if err != nil {
		return 1, err
	}
	ids := args[1:]
	if len(ids) == 0 {
		for _, p := range k.Platforms {
			if p.Auto() {
				ids = append(ids, p.ID)
			}
		}
		if len(ids) == 0 {
			return 1, errors.New("没有可自动更新的平台（GitHub Actions、Vercel）")
		}
	}
	e.syncAll(*k, v, ids)
	return 0, e.checklist(k.Name)
}

func audit(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	n := fs.Int("n", 50, "")
	key := fs.String("key", "", "")
	if _, err := parse(fs, args); err != nil {
		return 2, err
	}
	var keep func(vault.Event) bool
	if *key != "" {
		keep = vault.ForKey(*key)
	}
	evs := vault.ReadLog(e.Vault.Dir, *n, keep)
	if len(evs) == 0 {
		fmt.Fprintln(e.Stdout, "还没有访问记录。")
		return 0, nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "时间\t谁\t操作\t密钥\t结果\t详情")
	for _, ev := range evs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", ev.Time.Local().Format("01-02 15:04:05"), ev.Client, actionZh(ev.Action),
			strings.Join(ev.Keys, ","), vault.DecisionZh(ev.Decision), ev.Detail)
	}
	return 0, tw.Flush()
}

func actionZh(a string) string {
	if s, ok := map[string]string{"get": "读取", "reveal": "读明文", "run": "运行", "copy": "复制", "rotate": "轮换", "add": "添加",
		"rm": "删除", "sync": "同步", "import": "导入", "list": "列出"}[a]; ok {
		return s
	}
	return a
}

func doctor(e *Env, args []string) (int, error) {
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	now := e.now()
	problems := 0
	w := e.Stdout
	if err := e.Vault.Check(); err != nil {
		fmt.Fprintf(w, "✗ 钥匙串不可用：%v\n", err)
		problems++
	} else {
		fmt.Fprintf(w, "✓ 钥匙串可用（%s）\n", strings.TrimSpace(storeName()))
	}
	var over, soon, pend []string
	for _, k := range inv.Keys {
		st := inv.StatusOf(k, now)
		switch st.State {
		case vault.Overdue:
			over = append(over, k.Name+"（"+st.Why+"）")
		case vault.DueSoon:
			soon = append(soon, k.Name+"（"+st.Why+"）")
		}
		if st.Pending > 0 {
			pend = append(pend, fmt.Sprintf("%s（%d 个平台）", k.Name, st.Pending))
		}
	}
	report := func(ok, bad string, items []string) {
		if len(items) == 0 {
			fmt.Fprintln(w, "✓ "+ok)
			return
		}
		problems++
		fmt.Fprintf(w, "✗ %s：\n", bad)
		for _, s := range items {
			fmt.Fprintln(w, "    "+s)
		}
	}
	fmt.Fprintf(w, "  共 %d 个密钥\n", len(inv.Keys))
	report("没有超期的密钥", "超期，该轮换了（keys rotate NAME）", over)
	report("没有快到期的密钥", fmt.Sprintf("%d 天内到期", vault.SoonDays), soon)
	report("轮换清单都已完成", "轮换后还有平台没更新（keys show NAME）", pend)
	dirs := args
	if len(dirs) == 0 {
		wd, _ := e.Getwd()
		dirs = []string{wd}
	}
	var envs []string
	for _, d := range dirs {
		envs = append(envs, project.FindDotenvs(d, 5)...)
	}
	report("没找到明文 .env 文件（"+strings.Join(shortPaths(dirs), "、")+"）", "明文 .env 文件（keys import <文件> --shred 导入后删除）", shortPaths(envs))
	if p, ok := project.Find(dirs[0]); ok {
		f, err := project.Load(p)
		if err != nil {
			fmt.Fprintf(w, "✗ %v\n", err)
			problems++
		} else {
			var missing []string
			for _, k := range f.Keys() {
				if _, ok := inv.Find(k); !ok {
					missing = append(missing, k)
				}
			}
			report(shortPath(p)+" 用到的密钥都在", shortPath(p)+" 用到但 KeyBar 里没有", missing)
		}
	}
	if problems == 0 {
		fmt.Fprintln(w, "一切正常。")
		return 0, nil
	}
	return 1, nil
}

func shortPaths(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = shortPath(p)
	}
	return out
}

func mcpCmd(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	client := fs.String("client", "", "")
	if _, err := parse(fs, args); err != nil {
		return 2, err
	}
	s := &mcp.Server{Vault: e.Vault, Gate: e.Gate, Client: *client, Version: e.Version}
	if err := s.Serve(e.Stdin, e.Stdout); err != nil {
		return 1, err
	}
	return 0, nil
}

func config(e *Env, args []string) (int, error) {
	dir := e.Vault.Dir
	s := vault.LoadSettings(dir)
	inv, err := e.Vault.Inventory()
	if err != nil {
		return 1, err
	}
	if len(args) == 0 {
		days := inv.RotateDays
		if days == 0 {
			days = vault.DefaultRotateDays
		}
		fmt.Fprintf(e.Stdout, "rotate-days    %d    默认轮换周期（天）\n", days)
		fmt.Fprintf(e.Stdout, "remind         %v    到期提醒（KeyBar 每天检查一次）\n", onOff(s.Remind))
		fmt.Fprintf(e.Stdout, "remind-days    %d    提前几天提醒\n", s.RemindDays)
		fmt.Fprintf(e.Stdout, "grant-minutes  %d    「限时允许」的分钟数\n", s.GrantMinutes)
		fmt.Fprintf(e.Stdout, "cli-approval   %v    非交互的 keys get / run 也要批准\n", onOff(s.CLIApproval))
		fmt.Fprintf(e.Stdout, "数据目录：%s\n", shortPath(dir))
		return 0, nil
	}
	if len(args) != 2 {
		return 2, usageError("用法：keys config [名 值]")
	}
	name, val := args[0], args[1]
	num := func() (int, error) {
		n, err := strconv.Atoi(val)
		if err != nil || n == 0 {
			return 0, usageError(name + " 要一个非零整数")
		}
		return n, nil
	}
	switch name {
	case "rotate-days":
		n, err := num()
		if err != nil {
			return 2, err
		}
		if err := e.Vault.Update(func(inv *vault.Inventory) error { inv.RotateDays = n; return nil }); err != nil {
			return 1, err
		}
	case "remind":
		s.Remind = val == "on" || val == "true" || val == "1"
	case "remind-days":
		n, err := num()
		if err != nil {
			return 2, err
		}
		s.RemindDays = n
	case "grant-minutes":
		n, err := num()
		if err != nil {
			return 2, err
		}
		s.GrantMinutes = max(1, min(n, 24*60))
	case "cli-approval":
		// Only from a terminal: an agent's shell must not turn it off.
		if !e.terminal() {
			return 1, errors.New("cli-approval 只能在终端里或 KeyBar 设置里修改")
		}
		s.CLIApproval = val == "on" || val == "true" || val == "1"
	default:
		return 2, usageError("没有设置 " + name)
	}
	if err := vault.SaveSettings(dir, s); err != nil {
		return 1, err
	}
	fmt.Fprintf(e.Stdout, "%s = %s\n", name, val)
	return 0, nil
}

func onOff(b bool) string {
	if b {
		return "on "
	}
	return "off"
}
