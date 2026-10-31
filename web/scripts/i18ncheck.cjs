// Reports translation keys used in the .ts and .vue files that the English
// messages (src/locales/en.ts) lack, and exits with status 1 when it finds
// any. The Polish messages have the type of the English ones, so the type
// check keeps them complete. Keys built at run time are checked up to the
// first ${...}, e.g. `state.${key}` needs a group "state".
// Usage: npm run i18ncheck
const fs = require('fs')
const path = require('path')
const ts = require('typescript')
const web = path.resolve(__dirname, '..')

function load(file) {
  const source = fs.readFileSync(file, 'utf8')
  const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} }
  new Function('module', 'exports', 'require', js)(module, module.exports, require)
  return module.exports.default
}

function flatten(obj, prefix, keys) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix + k
    if (v && typeof v === 'object') flatten(v, key + '.', keys)
    else keys.add(key)
  }
  return keys
}

function files(dir, acc = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) files(p, acc)
    else if (/\.(ts|vue)$/.test(e.name) && !/\.test\.ts$/.test(e.name)) acc.push(p)
  }
  return acc
}

const keys = flatten(load(path.join(web, 'src', 'locales', 'en.ts')), '', new Set())
const known = (key) => keys.has(key)
const knownPrefix = (prefix) => [...keys].some((k) => k.startsWith(prefix))

// t('a.b'), $t("a.b"), te('a.b'), i18n.t(`a.${x}`)
const call = /\b(?:\$t|t|te)\(\s*(['"`])((?:(?!\1).)*)\1/g
const out = []
for (const file of files(path.join(web, 'src'))) {
  if (file.includes(path.join('src', 'locales'))) continue
  const lines = fs.readFileSync(file, 'utf8').split('\n')
  lines.forEach((line, i) => {
    for (const m of line.matchAll(call)) {
      const [, quote, key] = m
      const dynamic = quote === '`' && key.includes('${')
      const ok = dynamic ? knownPrefix(key.slice(0, key.indexOf('${'))) : known(key)
      if (!ok) out.push(`${path.relative(web, file)}:${i + 1}\tunknown translation key ${quote}${key}${quote}`)
    }
  })
}

if (out.length) {
  console.log(out.join('\n'))
  process.exit(1)
}
console.log(`${keys.size} translation keys, all used keys known`)
