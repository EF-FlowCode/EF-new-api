import { rspack, type Rspack } from '@rsbuild/core'

export class ResourceRecoveryPlugin {
  apply(compiler: Rspack.Compiler) {
    const name = 'ResourceRecoveryPlugin'
    compiler.hooks.thisCompilation.tap(name, (compilation) => {
      compilation.hooks.runtimeRequirementInTree
        .for(rspack.RuntimeGlobals.ensureChunk)
        .tap(name, (chunk) => {
          compilation.addRuntimeModule(chunk, new ResourceRecoveryRuntime())
        })
    })
  }
}

class ResourceRecoveryRuntime extends rspack.RuntimeModule {
  constructor() {
    super('resource recovery', rspack.RuntimeModule.STAGE_ATTACH)
  }

  generate() {
    return `
var recovery = window.__newApiResourceRecovery;
if (recovery) {
  var ensure = __webpack_require__.e;
  __webpack_require__.e = function(chunkId, fetchPriority) {
    return recovery.run(chunkId, function() { return ensure(chunkId, fetchPriority); });
  };
}`
  }
}
