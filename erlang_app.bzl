load(":app_file.bzl", "app_file")
load(":erlang_bytecode.bzl", "erlang_bytecode")
load(
    ":erlang_app_info.bzl",
    "erlang_app_info",
    _ErlangAppInfo = "ErlangAppInfo",
)
load(
    "//tools:erlang.bzl",
    "DEFAULT_ERLANG_INSTALLATION",
    "installation_suffix",
)

DEFAULT_ERLC_OPTS = [
    "-Werror",
    "+deterministic",
    "+debug_info",
    "+warn_export_vars",
    "+warn_shadow_vars",
    "+warn_obsolete_guard",
]

DEFAULT_TEST_ERLC_OPTS = [
    "+deterministic",
    "+debug_info",
    "+warn_export_vars",
    "+warn_shadow_vars",
    "+warn_obsolete_guard",
    "-DTEST=1",
]

ErlangAppInfo = _ErlangAppInfo

def erlang_app(
        erlang_installations = [DEFAULT_ERLANG_INSTALLATION],
        app_name = "",
        app_version = "",
        app_description = "",
        app_module = "",
        app_registered = [],
        app_env = "",
        app_extra_keys = "",
        extra_apps = [],
        erlc_opts = DEFAULT_ERLC_OPTS,
        first_srcs = [],
        extra_priv = [],
        extra_license_files = [],
        build_deps = [],
        deps = [],
        runtime_deps = [],
        stamp = -1):
    for erlang_installation in erlang_installations:
        suffix = installation_suffix(erlang_installation)

        all_beam = []

        if len(first_srcs) > 0:
            all_beam = [":first_beam_files-{}".format(suffix)]
            erlang_bytecode(
                name = "first_beam_files-{}".format(suffix),
                erlang_installation = erlang_installation,
                hdrs = native.glob(["include/**/*.hrl", "src/**/*.hrl"]),
                srcs = native.glob(first_srcs),
                erlc_opts = erlc_opts,
                dest = "ebin-{}".format(suffix),
                deps = build_deps + deps,
            )

        erlang_bytecode(
            name = "beam_files-{}".format(suffix),
            erlang_installation = erlang_installation,
            hdrs = native.glob(["include/**/*.hrl", "src/**/*.hrl"]),
            srcs = native.glob(["src/**/*.erl"], exclude = first_srcs),
            beam = all_beam,
            erlc_opts = erlc_opts,
            dest = "ebin-{}".format(suffix),
            deps = build_deps + deps,
        )

        all_beam = all_beam + [":beam_files-{}".format(suffix)]

        if len(native.glob(["ebin/{}.app".format(app_name)])) == 0:
            app_file(
                name = "app_file-{}".format(suffix),
                erlang_installation = erlang_installation,
                app_name = app_name,
                app_version = app_version,
                app_description = app_description,
                app_module = app_module,
                app_registered = app_registered,
                app_env = app_env,
                app_extra_keys = app_extra_keys,
                extra_apps = extra_apps,
                app_src = native.glob(["src/{}.app.src".format(app_name)]),
                modules = all_beam,
                deps = deps + runtime_deps,
                dest = "ebin-{}".format(suffix),
                stamp = stamp,
            )
            app = ":app_file-{}".format(suffix)
        else:
            app = "ebin/{}.app".format(app_name)

        erlang_app_info(
            name = "erlang_app-{}".format(suffix),
            app_name = app_name,
            hdrs = native.glob(["include/**/*.hrl"]),
            app = app,
            beam = all_beam,
            priv = native.glob(["priv/**/*"]) + extra_priv,
            license_files = native.glob(["LICENSE*"]) + extra_license_files,
            deps = deps + runtime_deps,
            visibility = ["//visibility:public"],
        )

def test_erlang_app(
        erlang_installations = [DEFAULT_ERLANG_INSTALLATION],
        app_name = "",
        app_version = "",
        app_description = "",
        app_module = "",
        app_registered = [],
        app_env = "",
        app_extra_keys = "",
        extra_apps = [],
        erlc_opts = DEFAULT_TEST_ERLC_OPTS,
        first_srcs = [],
        extra_priv = [],
        extra_license_files = [],
        build_deps = [],
        deps = [],
        runtime_deps = []):
    for erlang_installation in erlang_installations:
        suffix = installation_suffix(erlang_installation)

        all_test_beam = []

        if len(first_srcs) > 0:
            all_test_beam = [":first_test_beam_files-{}".format(suffix)]
            erlang_bytecode(
                name = "first_test_beam_files-{}".format(suffix),
                erlang_installation = erlang_installation,
                hdrs = native.glob(["include/**/*.hrl", "src/**/*.hrl"]),
                srcs = native.glob(first_srcs),
                erlc_opts = erlc_opts,
                dest = "test-{}".format(suffix),
                deps = build_deps + deps,
                testonly = True,
            )

        erlang_bytecode(
            name = "test_beam_files-{}".format(suffix),
            erlang_installation = erlang_installation,
            hdrs = native.glob(["include/**/*.hrl", "src/**/*.hrl"]),
            srcs = native.glob(["src/**/*.erl"], exclude = first_srcs),
            beam = all_test_beam,
            erlc_opts = erlc_opts,
            dest = "test-{}".format(suffix),
            deps = build_deps + deps,
            testonly = True,
        )

        all_test_beam = all_test_beam + [":test_beam_files-{}".format(suffix)]

        if len(native.glob(["ebin/{}.app".format(app_name)])) == 0:
            app = ":app_file-{}".format(suffix)
        else:
            app = "ebin/{}.app".format(app_name)

        erlang_app_info(
            name = "test_erlang_app-{}".format(suffix),
            app_name = app_name,
            hdrs = native.glob(["include/**/*.hrl", "src/**/*.hrl"]),
            app = app,
            beam = all_test_beam,
            priv = native.glob(["priv/**/*"]) + extra_priv,
            license_files = native.glob(["LICENSE*"]) + extra_license_files,
            deps = deps + runtime_deps,
            visibility = ["//visibility:public"],
            testonly = True,
        )
