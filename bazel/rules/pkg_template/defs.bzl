"""Rule for applying gopatch to a set of Go source files."""

def _apply_gopatch_impl(ctx):
    out = ctx.actions.declare_directory(ctx.label.name)

    args = ctx.actions.args()
    args.add("-gopatch", ctx.executable.gopatch)
    for p in ctx.files.patches:
        args.add("-p", p)
    args.add("-o", out.path)
    args.add_all(ctx.files.srcs)

    ctx.actions.run(
        executable = ctx.executable._wrapper,
        inputs = depset(ctx.files.srcs + ctx.files.patches),
        outputs = [out],
        arguments = [args],
        tools = [ctx.attr.gopatch[DefaultInfo].files_to_run],
        mnemonic = "ApplyGopatch",
    )

    return [DefaultInfo(files = depset([out]))]

apply_gopatch = rule(
    implementation = _apply_gopatch_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "patches": attr.label_list(allow_files = True),
        "gopatch": attr.label(
            executable = True,
            cfg = "exec",
        ),
        "_wrapper": attr.label(
            default = "//bazel/rules/pkg_template/run_gopatch:run_gopatch",
            executable = True,
            cfg = "exec",
        ),
    },
)
