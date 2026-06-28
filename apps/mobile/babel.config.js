// babel-preset-expo auto-detects and adds the reanimated/worklets plugin and the
// React Compiler (app.json `experiments.reactCompiler`), so we only add the
// NativeWind bits here: the jsxImportSource override + the nativewind babel preset.
module.exports = function (api) {
  api.cache(true);
  return {
    presets: [['babel-preset-expo', { jsxImportSource: 'nativewind' }], 'nativewind/babel'],
  };
};
