package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"keybar/importer"
	"keybar/scan"
	"keybar/vault"
)

// scanCmd: keys scan finds the keys written out in .env files and the
// shell's startup files, and with --import brings the likely ones in.
// Values are never printed: a prefix and the length at most.
func scanCmd(e *Env, args []string) (int, error) {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	roots := fs.String("roots", "", "")
	var files multi
	fs.Var(&files, "file", "")
	doImport := fs.Bool("import", false, "")
	all := fs.Bool("all", false, "")
	noRC := fs.Bool("no-rc", false, "")
	onConflict := fs.String("on-conflict", "", "")
	clean := fs.String("clean", "", "")
	pos, err := parse(fs, args)
	if err != nil {
		return 2, err
	}
	if len(pos) > 0 {
		return 2, usageError("用法：keys scan [--roots 目录,目录] [--file 文件]... [--no-rc] [--import [--all] [--on-conflict skip|overwrite|rename] [--clean comment|names|delete]]")
	}
	var choice *importer.Choice
	if *onConflict != "" {
		c, err := importer.ParseChoice(*onConflict)
		if err != nil {
			return 2, usageError(err.Error())
		}
		choice = &c
	}
	mode, err := scan.ParseMode(*clean)
	if err != nil {
		return 2, usageError(err.Error())
	}
	if *clean != "" && !*doImport {
		return 2, usageError("--clean 要和 --import 一起用：先进 KeyBar，再从文件里移除")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	set := vault.LoadSettings(e.Vault.Dir)
	o := scan.Options{Home: home, Roots: set.ScanRoots, Ignored: set.IgnoredFiles, NoRC: *noRC}
	if *roots != "" {
		o.Roots = strings.Split(*roots, ",")
	}
	var found []scan.Found
	if len(files) > 0 {
		for _, f := range files {
			p, _ := filepath.Abs(expandHome(f, home))
			ff, ok := scan.ReadEnv(p)
			if isRC(p, home) {
				ff, ok = scan.ReadRC(p)
			}
			if !ok {
				return 1, fmt.Errorf("%s 里没有带值的变量", shortPath(p))
			}
			found = append(found, ff)
		}
	} else {
		found = scan.Run(o)
	}
	groups, err := scan.Plans(e.Vault, found)
	if err != nil {
		return 1, err
	}
	likely := 0
	for _, g := range groups {
		for _, it := range g.Plan.Items {
			if *all && it.State != importer.Empty {
				it.Skip = false
			}
			if it.State == importer.Conflict && it.Elsewhere == "" {
				it.Choice = importer.Keep
				if choice != nil {
					it.Choice = *choice
				}
			} else if it.State == importer.Conflict && choice != nil {
				it.Choice = *choice
			}
			if it.Likely {
				likely++
			}
		}
	}
	_ = e.Vault.Log(vault.Event{Client: "cli", Action: "scan", Decision: "ok", Detail: fmt.Sprintf("%d 个文件，%d 个像密钥", len(groups), likely)})
	if len(groups) == 0 {
		fmt.Fprintln(e.Stdout, "没找到带值的 .env 或 shell 配置里的 export。")
		return 0, nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	for _, g := range groups {
		p := g.Plan
		fmt.Fprintf(tw, "%s\t(%s)\n", shortPath(p.Path), p.Project)
		for _, it := range p.Items {
			mark := "·"
			if !it.Skip && it.State != importer.Empty {
				mark = "✓"
			}
			fmt.Fprintf(tw, "  %s %s\t%s\t%s\n", mark, it.Var, it.Masked, stateZh(it))
		}
	}
	_ = tw.Flush()
	nfiles, keys := scan.Checked(groups)
	if !*doImport {
		fmt.Fprintf(e.Stdout, "找到 %d 个文件，%d 个像密钥；打勾的 %d 个（%d 个文件）。keys scan --import 导入打勾的，--all 全部。\n", len(groups), likely, keys, nfiles)
		return 0, nil
	}
	total := importer.Result{}
	var failed []string
	for _, g := range groups {
		if !checked(g) {
			continue
		}
		out := scan.Import(e.Vault, g, *clean != "", mode)
		if out.Err != nil {
			failed = append(failed, shortPath(out.Path)+"："+out.Err.Error())
			continue
		}
		r := out.Result
		_ = e.Vault.Log(vault.Event{Client: "cli", Action: "import", Keys: out.Keys(), Decision: "ok", Detail: out.Path})
		fmt.Fprintf(e.Stdout, "%s：%s\n", shortPath(out.Path), r.Summary())
		for _, f := range r.Failed {
			fmt.Fprintf(e.Stderr, "  ✗ %s\n", f)
		}
		if out.Mapping != "" {
			fmt.Fprintf(e.Stdout, "  改名的记在 %s\n", shortPath(out.Mapping))
		}
		switch {
		case out.CleanErr != nil:
			fmt.Fprintf(e.Stderr, "  没有移除明文：%v\n", out.CleanErr)
		case out.Cleaned > 0:
			_ = e.Vault.Log(vault.Event{Client: "cli", Action: "clean", Keys: out.Keys(), Decision: "ok", Detail: out.Path})
			fmt.Fprintf(e.Stdout, "  已从文件移除 %d 行明文；原文件备份在 %s（含明文，确认无误后删除）\n", out.Cleaned, shortPath(out.Backup))
		}
		total.Added += r.Added
		total.Renamed += r.Renamed
		total.Overwritten += r.Overwritten
		total.Same += r.Same
		total.Kept += r.Kept
		failed = append(failed, r.Failed...)
	}
	for _, f := range failed {
		fmt.Fprintf(e.Stderr, "✗ %s\n", f)
	}
	fmt.Fprintf(e.Stdout, "合计：%s。\n", total.Summary())
	if total.Kept > 0 && choice == nil {
		fmt.Fprintf(e.Stdout, "同名但值不同的 %d 个保留了 KeyBar 里的：要覆盖或改名，加 --on-conflict overwrite 或 rename。\n", total.Kept)
	}
	if len(failed) > 0 {
		return 1, nil
	}
	return 0, nil
}

func checked(g scan.Group) bool {
	for _, it := range g.Plan.Items {
		if !it.Skip && it.State != importer.Empty {
			return true
		}
	}
	return false
}

func stateZh(it *importer.Item) string {
	var s string
	switch it.State {
	case importer.Empty:
		return "空值"
	case importer.New:
		s = "新增"
	case importer.Same:
		s = "已有相同值"
		if it.Elsewhere != "" {
			s = "与 " + shortPath(it.Elsewhere) + " 相同"
		}
	case importer.Conflict:
		s = "同名不同值 → " + []string{"保留现有", "覆盖", "改名为 " + it.NewName}[it.Choice]
		if it.Elsewhere != "" {
			s = "与 " + shortPath(it.Elsewhere) + " 同名不同值 → " + []string{"保留那个", "覆盖", "改名为 " + it.NewName}[it.Choice]
		}
	}
	if !it.Likely {
		s += "（不像密钥）"
	}
	return s
}

func isRC(p, home string) bool {
	for _, r := range scan.RCFiles(home) {
		if r == p {
			return true
		}
	}
	return false
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
