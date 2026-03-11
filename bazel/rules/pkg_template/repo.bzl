_SDK_DIRS = [
    ("src/text/template", "text"),
    ("src/html/template", "html"),
    ("src/internal/fmtsort", "internal/fmtsort"),
]

_BUILD_BAZEL = """\
package(default_visibility = ["//visibility:public"])

filegroup(
    name = "text",
    srcs = glob(["text/*.go"], exclude = ["text/*_test.go"]),
)

filegroup(
    name = "html",
    srcs = glob(["html/*.go"], exclude = ["html/*_test.go"]),
)

filegroup(
    name = "internal_fmtsort",
    srcs = glob(["internal/fmtsort/*.go"], exclude = ["internal/fmtsort/*_test.go"]),
)
"""

def _pkg_template_gen_impl(rctx):
    sdk_root = rctx.path(Label("@go_sdk//:ROOT")).dirname

    for sdk_dir, out_dir in _SDK_DIRS:
        src = sdk_root.get_child(sdk_dir)
        result = rctx.execute(["mkdir", "-p", out_dir])
        if result.return_code != 0:
            fail("mkdir failed for {}: {}".format(out_dir, result.stderr))
        result = rctx.execute(["cp", "-r", str(src) + "/.", out_dir])
        if result.return_code != 0:
            fail("cp failed for {}: {}".format(sdk_dir, result.stderr))

    rctx.patch(Label("//pkg/template:no-method.patch"), strip = 3)
    rctx.patch(Label("//pkg/template:types.patch"), strip = 3)

    rctx.file("BUILD.bazel", _BUILD_BAZEL)

pkg_template_gen = repository_rule(
    implementation = _pkg_template_gen_impl,
)
