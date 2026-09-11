// Separate module, stdlib only. The snapshot generator locates the pinned API
// packages in the module cache and parses them as source; it does not link them,
// so none of the CAPI dependency graph reaches the CLI module.
module sixfields/hack/tools

go 1.25.4
