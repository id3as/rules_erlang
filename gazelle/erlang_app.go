package erlang

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

type ErlangApp struct {
	RepoRoot     string
	Rel          string
	Name         string
	Description  string
	Version      string
	Ebin         MutableSet[string]
	Srcs         MutableSet[string]
	PrivateHdrs  MutableSet[string]
	PublicHdrs   MutableSet[string]
	AppSrc       MutableSet[string]
	TestSrcs     MutableSet[string]
	TestHdrs     MutableSet[string]
	Priv         MutableSet[string]
	LicenseFiles MutableSet[string]
	ErlcOpts     MutableSet[string]
	TestErlcOpts MutableSet[string]
	Deps         MutableSet[string]
	ExtraApps    MutableSet[string]
}

var ignoredIncludeLoggingPattern = regexp.MustCompile(`/lib/[^-]+-[^/]+/include/`)

func NewErlangApp(repoRoot, rel string) *ErlangApp {
	return &ErlangApp{
		RepoRoot:     repoRoot,
		Rel:          rel,
		Ebin:         NewMutableSet[string](),
		Srcs:         NewMutableSet[string](),
		PrivateHdrs:  NewMutableSet[string](),
		PublicHdrs:   NewMutableSet[string](),
		AppSrc:       NewMutableSet[string](),
		TestSrcs:     NewMutableSet[string](),
		TestHdrs:     NewMutableSet[string](),
		Priv:         NewMutableSet[string](),
		LicenseFiles: NewMutableSet[string](),
		ErlcOpts:     NewMutableSet[string](),
		TestErlcOpts: NewMutableSet[string](),
		Deps:         NewMutableSet[string](),
		ExtraApps:    NewMutableSet[string](),
	}
}

func (erlangApp *ErlangApp) AddFile(f string) {
	if strings.HasPrefix(f, "ebin/") {
		if strings.HasSuffix(f, ".app") {
			erlangApp.Ebin.Add(f)
		}
		// TODO: handle .appup files
	} else if strings.HasPrefix(f, "src/") {
		if strings.HasSuffix(f, ".erl") {
			erlangApp.Srcs.Add(f)
		} else if strings.HasSuffix(f, ".hrl") {
			erlangApp.PrivateHdrs.Add(f)
		} else if strings.HasSuffix(f, ".app.src") {
			erlangApp.AppSrc.Add(f)
		}
	} else if strings.HasPrefix(f, "include/") {
		if strings.HasSuffix(f, ".hrl") {
			erlangApp.PublicHdrs.Add(f)
		}
	} else if strings.HasPrefix(f, "test/") {
		if strings.HasSuffix(f, ".erl") {
			erlangApp.TestSrcs.Add(f)
		} else if strings.HasSuffix(f, ".hrl") {
			erlangApp.TestHdrs.Add(f)
		}
	} else if strings.HasPrefix(f, "priv/") {
		erlangApp.Priv.Add(f)
	} else if strings.HasPrefix(f, "LICENSE") {
		erlangApp.LicenseFiles.Add(f)
	}
}

func moduleName(src string) string {
	base := filepath.Base(src)
	if strings.HasSuffix(base, ".erl") {
		return strings.TrimSuffix(base, ".erl")
	}
	return strings.TrimSuffix(base, ".beam")
}

func beamFile(src string) string {
	r := "ebin/" + filepath.Base(src)
	return strings.TrimSuffix(r, ".erl") + ".beam"
}

func testBeamFile(src string) string {
	r := "test/" + filepath.Base(src)
	return strings.TrimSuffix(r, ".erl") + ".beam"
}

func ruleName(f string) string {
	r := strings.ReplaceAll(f, string(filepath.Separator), "_")
	return strings.ReplaceAll(r, ".", "_")
}

func (erlangApp *ErlangApp) pathFor(from, include string) string {
	if erlangApp.PrivateHdrs.Contains(include) || erlangApp.PublicHdrs.Contains(include) {
		return include
	}

	directPath := filepath.Join(filepath.Dir(from), include)
	if erlangApp.PrivateHdrs.Contains(directPath) || erlangApp.PublicHdrs.Contains(directPath) {
		return directPath
	}

	privatePath := filepath.Join("src", include)
	if erlangApp.PrivateHdrs.Contains(privatePath) {
		return privatePath
	}
	publicPath := filepath.Join("include", include)
	if erlangApp.PublicHdrs.Contains(publicPath) {
		return publicPath
	}
	return ""
}

func erlcOptsWithSelect(opts MutableSet[string]) rule.SelectStringListValue {
	debugOpts := Copy(opts)
	defaultOpts := Copy(opts)
	defaultOpts.Add("+deterministic")
	return rule.SelectStringListValue{
		"@rules_erlang//:debug_build": debugOpts.Values(strings.Compare),
		"//conditions:default":        defaultOpts.Values(strings.Compare),
	}
}

func (erlangApp *ErlangApp) ErlcOptsRule() *rule.Rule {
	erlc_opts := rule.NewRule(erlcOptsKind, erlcOptsRuleName)
	erlc_opts.SetAttr("values", erlcOptsWithSelect(erlangApp.ErlcOpts))
	erlc_opts.SetAttr("visibility", []string{":__subpackages__"})
	return erlc_opts
}

func (erlangApp *ErlangApp) testErlcOptsRule() *rule.Rule {
	test_erlc_opts := rule.NewRule(erlcOptsKind, testErlcOptsRuleName)
	test_erlc_opts.SetAttr("values", erlcOptsWithSelect(erlangApp.TestErlcOpts))
	test_erlc_opts.SetAttr("visibility", []string{":__subpackages__"})
	return test_erlc_opts
}

func (erlangApp *ErlangApp) basePltRule() *rule.Rule {
	plt := rule.NewRule(pltKind, "base_plt")
	plt.SetAttr("visibility", []string{":__subpackages__"})
	return plt
}

func macros(erlcOpts MutableSet[string]) ErlParserMacros {
	r := make(ErlParserMacros)
	erlcOpts.ForEach(func(opt string) {
		if strings.HasPrefix(opt, "-D") {
			parts := strings.Split(strings.TrimPrefix(opt, "-D"), "=")
			if len(parts) == 1 {
				r[parts[0]] = nil
			} else {
				r[parts[0]] = &parts[1]
			}
		}
	})
	return r
}

func (erlangApp *ErlangApp) BeamFilesRules(args language.GenerateArgs, erlParser ErlParser) (beamFilesRules []*rule.Rule) {
	erlangConfig := erlangConfigForRel(args.Config, args.Rel)

	ownModules := NewMutableSet[string]()
	for _, src := range erlangApp.Srcs.Values(strings.Compare) {
		ownModules.Add(moduleName(src))
	}

	moduleindex, err := ReadModuleindex(filepath.Join(args.Config.RepoRoot, "moduleindex.yaml"))
	if err != nil {
		moduleindex = map[string][]string{erlangApp.Name: ownModules.Values(strings.Compare)}
	}

	outs := NewMutableSet[string]()
	for _, src := range erlangApp.Srcs.Values(strings.Compare) {
		actualPath := filepath.Join(erlangApp.RepoRoot, erlangApp.Rel, src)
		// TODO: not print Parsing when the file does not exist
		Log(args.Config, "        Parsing", src, "->", actualPath)
		erlAttrs, err := erlParser.DeepParseErl(src, erlangApp, macros(erlangApp.ErlcOpts))
		if err != nil {
			log.Fatalf("ERROR: %v\n", err)
		}

		theseHdrs := NewMutableSet[string]()
		for _, include := range erlAttrs.Include {
			path := erlangApp.pathFor(src, include)
			if path != "" {
				Log(args.Config, "            include", path)
				theseHdrs.Add(path)
			} else if !ignoredIncludeLoggingPattern.MatchString(include) {
				Log(args.Config, "            ignoring include",
					include, "as it cannot be found")
			}
		}

		theseDeps := NewMutableSet[string]()
		for _, include := range erlAttrs.IncludeLib {
			path := erlangApp.pathFor(src, include)
			if path != "" {
				Log(args.Config, "            include_lib", path)
				theseHdrs.Add(path)
			} else if parts := strings.Split(include, string(os.PathSeparator)); len(parts) > 0 {
				if parts[0] == erlangApp.Name {
					path := erlangApp.pathFor(src, strings.Join(parts[1:], string(os.PathSeparator)))
					if path != "" {
						Log(args.Config, "            include_lib (self)", path)
						theseHdrs.Add(path)
					} else {
						Log(args.Config, "            ignoring include_lib (self)",
							include, "as it cannot be found")
					}
				} else if !erlangConfig.IgnoredDeps.Contains(parts[0]) {
					Log(args.Config, "            include_lib", include, "->", parts[0])
					theseDeps.Add(parts[0])
				} else {
					Log(args.Config, "            ignoring include_lib", include)
				}
			}
		}

		theseBeam := NewMutableSet[string]()
		for _, module := range erlAttrs.modules() {
			found := false
			for _, other_src := range erlangApp.Srcs.Values(strings.Compare) {
				if moduleName(other_src) == module {
					Log(args.Config, "            module", module, "->", beamFile(src))
					theseBeam.Add(beamFile(other_src))
					found = true
					break
				}
			}
			if found {
				continue
			}
			if dep, found := erlangConfig.ModuleMappings[module]; found {
				Log(args.Config, "            module", module, "->", fmt.Sprintf("%s:%s", dep, module))
				theseDeps.Add(dep)
				continue
			}
			if app := FindModule(moduleindex, module); app != "" && app != erlangApp.Name {
				Log(args.Config, "            module", module, "->", fmt.Sprintf("%s:%s", app, module))
				theseDeps.Add(app)
				continue
			}
		}

		for module := range erlAttrs.Call {
			app := erlangConfig.ModuleMappings[module]
			if app == "" {
				app = FindModule(moduleindex, module)
			}
			if app != "" && app != erlangApp.Name && !erlangConfig.IgnoredDeps.Contains(app) {
				Log(args.Config, "            call", module, "->", fmt.Sprintf("%s:%s", app, module))
				erlangApp.Deps.Add(app)
			} else {
				Log(args.Config, "            ignoring call", module, "->", app)
			}
		}

		out := beamFile(src)
		outs.Add(out)

		erlang_bytecode := rule.NewRule(erlangBytecodeKind, ruleName(out))
		erlang_bytecode.SetAttr("app_name", erlangApp.Name)
		erlang_bytecode.SetAttr("srcs", []interface{}{src})
		if !theseHdrs.IsEmpty() {
			erlang_bytecode.SetAttr("hdrs", theseHdrs.Values(strings.Compare))
		}
		erlang_bytecode.SetAttr("erlc_opts", "//:"+erlcOptsRuleName)
		erlang_bytecode.SetAttr("outs", []string{out})
		if !theseBeam.IsEmpty() {
			erlang_bytecode.SetAttr("beam", theseBeam.Values(strings.Compare))
		}
		if !theseDeps.IsEmpty() {
			erlang_bytecode.SetAttr("deps", theseDeps.Values(strings.Compare))
		}

		beamFilesRules = append(beamFilesRules, erlang_bytecode)
	}

	beam_files := rule.NewRule("filegroup", "beam_files")
	beam_files.SetAttr("srcs", outs.Values(strings.Compare))
	beamFilesRules = append(beamFilesRules, beam_files)
	return
}

func (erlangApp *ErlangApp) testBeamFilesRules(args language.GenerateArgs, erlParser ErlParser) (testBeamFilesRules []*rule.Rule) {
	erlangConfig := erlangConfigForRel(args.Config, args.Rel)

	ownModules := NewMutableSet[string]()
	for _, src := range erlangApp.Srcs.Values(strings.Compare) {
		ownModules.Add(moduleName(src))
	}

	moduleindex, err := ReadModuleindex(filepath.Join(args.Config.RepoRoot, "moduleindex.yaml"))
	if err != nil {
		moduleindex = map[string][]string{erlangApp.Name: ownModules.Values(strings.Compare)}
	}

	testOuts := NewMutableSet[string]()
	for _, src := range erlangApp.Srcs.Values(strings.Compare) {
		actualPath := filepath.Join(erlangApp.RepoRoot, erlangApp.Rel, src)
		// TODO: not print Parsing when the file does not exist
		Log(args.Config, "        Parsing (for tests)", src, "->", actualPath)
		erlAttrs, err := erlParser.DeepParseErl(src, erlangApp, macros(erlangApp.TestErlcOpts))
		if err != nil {
			log.Fatalf("ERROR: %v\n", err)
		}

		theseHdrs := NewMutableSet[string]()
		for _, include := range erlAttrs.Include {
			path := erlangApp.pathFor(src, include)
			if path != "" {
				Log(args.Config, "            include", path)
				theseHdrs.Add(path)
			} else if !ignoredIncludeLoggingPattern.MatchString(include) {
				Log(args.Config, "            ignoring include",
					include, "as it cannot be found")
			}
		}

		theseDeps := NewMutableSet[string]()
		for _, include := range erlAttrs.IncludeLib {
			path := erlangApp.pathFor(src, include)
			if path != "" {
				Log(args.Config, "            include_lib", path)
				theseHdrs.Add(path)
			} else if parts := strings.Split(include, string(os.PathSeparator)); len(parts) > 0 {
				if parts[0] == erlangApp.Name {
					path := erlangApp.pathFor(src, strings.Join(parts[1:], string(os.PathSeparator)))
					if path != "" {
						Log(args.Config, "            include_lib (self)", path)
						theseHdrs.Add(path)
					} else {
						Log(args.Config, "            ignoring include_lib (self)",
							include, "as it cannot be found")
					}
				} else if !erlangConfig.IgnoredDeps.Contains(parts[0]) {
					Log(args.Config, "            include_lib", include, "->", parts[0])
					theseDeps.Add(parts[0])
				} else {
					Log(args.Config, "            ignoring include_lib", include)
				}
			}
		}

		theseBeam := NewMutableSet[string]()
		for _, module := range erlAttrs.modules() {
			found := false
			for _, other_src := range erlangApp.Srcs.Values(strings.Compare) {
				if moduleName(other_src) == module {
					Log(args.Config, "            module", module, "->", beamFile(src))
					theseBeam.Add(beamFile(other_src))
					found = true
					break
				}
			}
			if found {
				continue
			}
			if dep, found := erlangConfig.ModuleMappings[module]; found {
				Log(args.Config, "            module", module, "->", fmt.Sprintf("%s:%s", dep, module))
				theseDeps.Add(dep)
				continue
			}
			if app := FindModule(moduleindex, module); app != "" && app != erlangApp.Name {
				Log(args.Config, "            module", module, "->", fmt.Sprintf("%s:%s", app, module))
				theseDeps.Add(app)
				continue
			}
		}

		for module := range erlAttrs.Call {
			app := erlangConfig.ModuleMappings[module]
			if app == "" {
				app = FindModule(moduleindex, module)
			}
			if app != "" && app != erlangApp.Name && !erlangConfig.IgnoredDeps.Contains(app) {
				Log(args.Config, "            call", module, "->", fmt.Sprintf("%s:%s", app, module))
				erlangApp.Deps.Add(app)
			} else {
				Log(args.Config, "            ignoring call", module, "->", app)
			}
		}

		test_out := testBeamFile(src)
		testOuts.Add(test_out)

		test_erlang_bytecode := rule.NewRule(erlangBytecodeKind, ruleName(test_out))
		test_erlang_bytecode.SetAttr("app_name", erlangApp.Name)
		test_erlang_bytecode.SetAttr("srcs", []interface{}{src})
		if !theseHdrs.IsEmpty() {
			test_erlang_bytecode.SetAttr("hdrs", theseHdrs.Values(strings.Compare))
		}
		test_erlang_bytecode.SetAttr("erlc_opts", "//:"+testErlcOptsRuleName)
		test_erlang_bytecode.SetAttr("outs", []string{test_out})
		if !theseBeam.IsEmpty() {
			test_erlang_bytecode.SetAttr("beam", theseBeam.Values(strings.Compare))
		}
		if !theseDeps.IsEmpty() {
			test_erlang_bytecode.SetAttr("deps", theseDeps.Values(strings.Compare))
		}
		test_erlang_bytecode.SetAttr("testonly", true)

		testBeamFilesRules = append(testBeamFilesRules, test_erlang_bytecode)
	}

	test_beam_files := rule.NewRule("filegroup", "test_beam_files")
	test_beam_files.SetAttr("srcs", testOuts.Values(strings.Compare))
	test_beam_files.SetAttr("testonly", true)
	testBeamFilesRules = append(testBeamFilesRules, test_beam_files)
	return
}

func (erlangApp *ErlangApp) allSrcsRules() []*rule.Rule {
	var rules []*rule.Rule

	srcs := rule.NewRule("filegroup", "srcs")
	srcs.SetAttr("srcs", Union(erlangApp.Srcs, erlangApp.AppSrc).Values(strings.Compare))
	rules = append(rules, srcs)

	private_hdrs := rule.NewRule("filegroup", "private_hdrs")
	private_hdrs.SetAttr("srcs", erlangApp.PrivateHdrs.Values(strings.Compare))
	rules = append(rules, private_hdrs)

	public_hdrs := rule.NewRule("filegroup", "public_hdrs")
	public_hdrs.SetAttr("srcs", erlangApp.PublicHdrs.Values(strings.Compare))
	rules = append(rules, public_hdrs)

	priv := rule.NewRule("filegroup", "priv")
	priv.SetAttr("srcs", erlangApp.Priv.Values(strings.Compare))
	rules = append(rules, priv)

	licenses := rule.NewRule("filegroup", "licenses")
	licenses.SetAttr("srcs", erlangApp.LicenseFiles.Values(strings.Compare))
	rules = append(rules, licenses)

	hdrs := rule.NewRule("filegroup", "public_and_private_hdrs")
	hdrs.SetAttr("srcs", []string{
		":private_hdrs",
		":public_hdrs",
	})
	rules = append(rules, hdrs)

	all_srcs := rule.NewRule("filegroup", "all_srcs")
	all_srcs.SetAttr("srcs", []string{
		":srcs",
		":public_and_private_hdrs",
		// ":priv",
	})
	rules = append(rules, all_srcs)

	return rules
}

func (erlangApp *ErlangApp) erlangAppRule(explicitFiles bool) *rule.Rule {
	r := rule.NewRule(erlangAppKind, "erlang_app")
	r.SetAttr("app_name", erlangApp.Name)
	if erlangApp.Version != "" {
		r.SetAttr("app_version", erlangApp.Version)
	}
	if erlangApp.Description != "" {
		r.SetAttr("app_description", erlangApp.Description)
	}
	if !erlangApp.ExtraApps.IsEmpty() {
		r.SetAttr("extra_apps", erlangApp.ExtraApps.Values(strings.Compare))
	}

	r.SetAttr("beam_files", []string{":beam_files"})
	if !erlangApp.PublicHdrs.IsEmpty() {
		r.SetAttr("hdrs", []string{":public_hdrs"})
	}
	r.SetAttr("srcs", []string{":all_srcs"})

	if explicitFiles && !erlangApp.LicenseFiles.IsEmpty() {
		r.SetAttr("extra_license_files", erlangApp.LicenseFiles.Values(strings.Compare))
	}

	if !erlangApp.Deps.IsEmpty() {
		r.SetAttr("deps", erlangApp.Deps.Values(strings.Compare))
	}
	return r
}

func (erlangApp *ErlangApp) testErlangAppRule(explicitFiles bool) *rule.Rule {
	r := rule.NewRule(testErlangAppKind, "test_erlang_app")
	r.SetAttr("app_name", erlangApp.Name)
	if erlangApp.Version != "" {
		r.SetAttr("app_version", erlangApp.Version)
	}
	if erlangApp.Description != "" {
		r.SetAttr("app_description", erlangApp.Description)
	}
	if !erlangApp.ExtraApps.IsEmpty() {
		r.SetAttr("extra_apps", erlangApp.ExtraApps.Values(strings.Compare))
	}

	r.SetAttr("beam_files", []string{":test_beam_files"})
	if !erlangApp.PublicHdrs.IsEmpty() || !erlangApp.PrivateHdrs.IsEmpty() {
		r.SetAttr("hdrs", []string{":public_and_private_hdrs"})
	}
	r.SetAttr("srcs", []string{":all_srcs"})

	if explicitFiles && !erlangApp.LicenseFiles.IsEmpty() {
		r.SetAttr("extra_license_files", erlangApp.LicenseFiles.Values(strings.Compare))
	}

	if !erlangApp.Deps.IsEmpty() {
		r.SetAttr("deps", erlangApp.Deps.Values(strings.Compare))
	}
	return r
}

func ruleNameForTestSrc(f string) string {
	modName := moduleName(f)
	if strings.HasSuffix(modName, "_SUITE") {
		return modName + "_beam_files"
	} else {
		return ruleName(f)
	}
}

func (erlangApp *ErlangApp) testPathFor(from, include string) string {
	standardPath := erlangApp.pathFor(from, include)
	if standardPath != "" {
		return standardPath
	}
	directPath := filepath.Join(filepath.Dir(from), include)
	if erlangApp.TestHdrs.Contains(directPath) {
		return directPath
	}
	testPath := filepath.Join("test", include)
	if erlangApp.PrivateHdrs.Contains(testPath) {
		return testPath
	}
	return ""
}

func (erlangApp *ErlangApp) TestDirBeamFilesRules(args language.GenerateArgs, erlParser ErlParser) []*rule.Rule {
	erlangConfig := erlangConfigForRel(args.Config, args.Rel)

	ownModules := NewMutableSet[string]()
	for _, src := range erlangApp.Srcs.Values(strings.Compare) {
		ownModules.Add(moduleName(src))
	}

	moduleindex, err := ReadModuleindex(filepath.Join(args.Config.RepoRoot, "moduleindex.yaml"))
	if err != nil {
		moduleindex = map[string][]string{erlangApp.Name: ownModules.Values(strings.Compare)}
	}

	var beamFilesRules []*rule.Rule
	outs := NewMutableSet[string]()
	for _, src := range erlangApp.TestSrcs.Values(strings.Compare) {
		actualPath := filepath.Join(erlangApp.RepoRoot, erlangApp.Rel, src)
		Log(args.Config, "        Parsing", src, "->", actualPath)
		erlAttrs, err := erlParser.DeepParseErl(src, erlangApp, macros(erlangApp.TestErlcOpts))
		if err != nil {
			log.Fatalf("ERROR: %v\n", err)
		}

		theseHdrs := NewMutableSet[string]()
		for _, include := range erlAttrs.Include {
			path := erlangApp.testPathFor(src, include)
			if path != "" {
				Log(args.Config, "            include", path)
				theseHdrs.Add(path)
			} else if !ignoredIncludeLoggingPattern.MatchString(include) {
				Log(args.Config, "            ignoring include",
					include, "as it cannot be found")
			}
		}

		theseDeps := NewMutableSet[string]()
		for _, include := range erlAttrs.IncludeLib {
			path := erlangApp.pathFor(src, include)
			if path != "" {
				Log(args.Config, "            include_lib", path)
				theseHdrs.Add(path)
			} else if parts := strings.Split(include, string(os.PathSeparator)); len(parts) > 0 {
				if !erlangConfig.IgnoredDeps.Contains(parts[0]) {
					Log(args.Config, "            include_lib", include, "->", parts[0])
					theseDeps.Add(parts[0])
				} else {
					Log(args.Config, "            ignoring include_lib", include)
				}
			}
		}

		theseBeam := NewMutableSet[string]()
		for _, module := range erlAttrs.modules() {
			found := false
			for _, other_src := range erlangApp.Srcs.Values(strings.Compare) {
				if moduleName(other_src) == module {
					Log(args.Config, "            module", module, "->", beamFile(src))
					theseBeam.Add(beamFile(other_src))
					found = true
					break
				}
			}
			if found {
				continue
			}
			if dep, found := erlangConfig.ModuleMappings[module]; found {
				Log(args.Config, "            module", module, "->", fmt.Sprintf("%s:%s", dep, module))
				theseDeps.Add(dep)
				continue
			}
			if app := FindModule(moduleindex, module); app != "" && app != erlangApp.Name {
				Log(args.Config, "            module", module, "->", fmt.Sprintf("%s:%s", app, module))
				theseDeps.Add(app)
				continue
			}
		}

		theseRuntimeBeam := NewMutableSet[string]()
		theseRuntimeDeps := NewMutableSet[string]()
		for module := range erlAttrs.Call {
			found := false
			for _, other_src := range erlangApp.TestSrcs.Values(strings.Compare) {
				if moduleName(other_src) == module {
					Log(args.Config, "            module", module, "->", beamFile(src))
					theseRuntimeBeam.Add(strings.TrimSuffix(other_src, ".erl") + ".beam")
					found = true
					break
				}
			}
			if found {
				continue
			}
			app := erlangConfig.ModuleMappings[module]
			if app == "" {
				app = FindModule(moduleindex, module)
			}
			if app != "" && app != erlangApp.Name && !erlangConfig.IgnoredDeps.Contains(app) {
				Log(args.Config, "            call", module, "->", fmt.Sprintf("%s:%s", app, module))
				theseRuntimeDeps.Add(app)
			} else {
				Log(args.Config, "            ignoring call", module, "->", app)
			}
		}

		out := strings.TrimSuffix(src, ".erl") + ".beam"
		outs.Add(out)

		erlang_bytecode := rule.NewRule(erlangBytecodeKind, ruleNameForTestSrc(out))
		erlang_bytecode.SetAttr("srcs", []interface{}{src})
		if !theseHdrs.IsEmpty() {
			erlang_bytecode.SetAttr("hdrs", theseHdrs.Values(strings.Compare))
		}
		erlang_bytecode.SetAttr("erlc_opts", "//:"+testErlcOptsRuleName)
		erlang_bytecode.SetAttr("outs", []string{out})
		if !theseBeam.IsEmpty() {
			erlang_bytecode.SetAttr("beam", theseBeam.Values(strings.Compare))
		}
		if !theseDeps.IsEmpty() {
			erlang_bytecode.SetAttr("deps", theseDeps.Values(strings.Compare))
		}
		erlang_bytecode.SetPrivateAttr("runtime_beam", theseRuntimeBeam.Values(strings.Compare))
		erlang_bytecode.SetPrivateAttr("runtime_deps", theseRuntimeDeps.Values(strings.Compare))
		erlang_bytecode.SetAttr("testonly", true)

		beamFilesRules = append(beamFilesRules, erlang_bytecode)
	}

	return beamFilesRules
}

func (erlangApp *ErlangApp) xrefRule() *rule.Rule {
	r := rule.NewRule(xrefKind, "xref")
	r.SetAttr("target", ":erlang_app")
	return r
}

func (erlangApp *ErlangApp) appPltRule() *rule.Rule {
	r := rule.NewRule(pltKind, "deps_plt")
	r.SetAttr("plt", "//:base_plt")
	r.SetAttr("for_target", ":erlang_app")
	return r
}

func (erlangApp *ErlangApp) dialyzeRule() *rule.Rule {
	r := rule.NewRule(dialyzeKind, "dialyze")
	r.SetAttr("target", ":erlang_app")
	r.SetAttr("plt", ":deps_plt")
	return r
}

func (erlangApp *ErlangApp) EunitRule() *rule.Rule {
	// eunit_mods is the list of source modules, plus any test module which is
	// not among the source modules with a "_tests" suffix appended
	modMap := make(map[string]string)
	for src := range erlangApp.Srcs {
		modMap[moduleName(src)] = ""
	}
	for testSrc := range erlangApp.TestSrcs {
		tm := moduleName(testSrc)
		if !strings.HasSuffix(tm, "_SUITE") {
			label := ":" + ruleNameForTestSrc(strings.TrimSuffix(testSrc, ".erl")+".beam")
			if strings.HasSuffix(tm, "_tests") {
				if _, ok := modMap[strings.TrimSuffix(tm, "_tests")]; ok {
					modMap[strings.TrimSuffix(tm, "_tests")] = label
				} else {
					modMap[tm] = label
				}
			} else {
				modMap[tm] = label
			}
		}
	}

	eunit_mods := NewMutableSet[string]()
	compiled_suites := NewMutableSet[string]()
	for mod, beam := range modMap {
		eunit_mods.Add(mod)
		if beam != "" {
			compiled_suites.Add(beam)
		}
	}

	eunit := rule.NewRule(eunitKind, "eunit")
	if !compiled_suites.IsEmpty() {
		eunit.SetAttr("compiled_suites", compiled_suites.Values(strings.Compare))
	}
	eunit.SetAttr("target", ":test_erlang_app")

	return eunit
}

func (erlangApp *ErlangApp) CtSuiteRules(testDirBeamFilesRules []*rule.Rule) []*rule.Rule {
	rulesByName := make(map[string]*rule.Rule, len(testDirBeamFilesRules))
	for _, r := range testDirBeamFilesRules {
		name := strings.TrimSuffix(r.Name(), "_beam_files")
		rulesByName[name] = r
	}

	var rules []*rule.Rule
	for _, testSrc := range erlangApp.TestSrcs.Values(strings.Compare) {
		modName := moduleName(testSrc)
		if strings.HasSuffix(modName, "_SUITE") {
			beamFilesRule := rulesByName[modName]
			r := rule.NewRule(ctTestKind, modName)
			r.SetAttr("compiled_suites",
				append([]string{":" + beamFilesRule.Name()},
					runtimeBeam(beamFilesRule)...))
			r.SetAttr("data", rule.GlobValue{
				Patterns: []string{"test/" + modName + "_data/**/*"},
			})
			deps := []string{":test_erlang_app"}
			deps = append(deps, runtimeDeps(beamFilesRule)...)
			r.SetAttr("deps", deps)

			rules = append(rules, r)
		}
	}

	return rules
}

func (erlangApp *ErlangApp) hasTestSuites() bool {
	return !erlangApp.TestSrcs.IsEmpty()
}

func (erlangApp *ErlangApp) modules() []string {
	modules := make([]string, len(erlangApp.Srcs))
	for i, src := range erlangApp.Srcs.Values(strings.Compare) {
		modules[i] = strings.TrimSuffix(filepath.Base(src), ".erl")
	}
	return modules
}

func runtimeBeam(r *rule.Rule) []string {
	return r.PrivateAttr("runtime_beam").([]string)
}

func runtimeDeps(r *rule.Rule) []string {
	return r.PrivateAttr("runtime_deps").([]string)
}