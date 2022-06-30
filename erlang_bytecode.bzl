load(
    "//private:erlang_bytecode.bzl",
    _erlang_bytecode = "erlang_bytecode",
)

def erlang_bytecode(**kwargs):
    if 'compile_first' in kwargs:
        _erlang_bytecode(**kwargs)
    else:
        _erlang_bytecode(
        compile_first = Label("//tools/compile_first:compile_first"),
            **kwargs
        )
