// Reports declarations that shadow a name of an enclosing scope in the .ts
// files and in the scripts and templates (v-for, slots) of the .vue files,
// and exits with status 1 when it finds any. Usage: npm run shadowcheck
const fs = require('fs')
const path = require('path')
const ts = require('typescript')
const sfc = require('@vue/compiler-sfc')
const web = path.resolve(__dirname, '..')

const out = []
const K = ts.SyntaxKind
const isFunctionLike = (n) => [K.FunctionDeclaration, K.FunctionExpression, K.ArrowFunction, K.MethodDeclaration, K.Constructor, K.GetAccessor, K.SetAccessor].includes(n.kind)
const isBlockScope = (n) => [K.Block, K.ForStatement, K.ForInStatement, K.ForOfStatement, K.CatchClause, K.CaseBlock].includes(n.kind)
const isTypeLevel = (n) => [K.InterfaceDeclaration, K.TypeAliasDeclaration, K.TypeLiteral, K.FunctionType, K.ConstructorType, K.TypeReference, K.TypeQuery, K.MappedType, K.IndexedAccessType, K.UnionType, K.IntersectionType, K.TypeOperator, K.TupleType, K.ArrayType, K.LiteralType, K.TypeParameter].includes(n.kind)

function bindings(name, acc = []) {
  if (!name) return acc
  if (ts.isIdentifier(name)) acc.push(name)
  else if (ts.isObjectBindingPattern(name) || ts.isArrayBindingPattern(name)) {
    for (const el of name.elements) if (ts.isBindingElement(el)) bindings(el.name, acc)
  }
  return acc
}

// walk collects the declarations of one scope and the scopes nested in it.
function walk(node, decls, children) {
  if (isFunctionLike(node)) {
    if (ts.isFunctionDeclaration(node) && node.name) decls.push({ id: node.name, kind: 'function' })
    children.push(node)
    return
  }
  if (isBlockScope(node)) {
    children.push(node)
    return
  }
  if (isTypeLevel(node)) return
  if (ts.isImportDeclaration(node)) {
    const c = node.importClause
    if (c && !c.isTypeOnly) {
      if (c.name) decls.push({ id: c.name, kind: 'import' })
      const nb = c.namedBindings
      if (nb && ts.isNamespaceImport(nb)) decls.push({ id: nb.name, kind: 'import' })
      if (nb && ts.isNamedImports(nb)) for (const s of nb.elements) if (!s.isTypeOnly) decls.push({ id: s.name, kind: 'import' })
    }
    return
  }
  if (ts.isVariableDeclaration(node)) {
    for (const id of bindings(node.name)) decls.push({ id, kind: 'variable' })
    if (node.initializer) walk(node.initializer, decls, children)
    return
  }
  if (ts.isClassDeclaration(node) && node.name) decls.push({ id: node.name, kind: 'class' })
  ts.forEachChild(node, (c) => walk(c, decls, children))
}

function scopeOf(node) {
  const decls = []
  const children = []
  if (isFunctionLike(node)) {
    for (const p of node.parameters) for (const id of bindings(p.name)) if (id.text !== 'this') decls.push({ id, kind: 'parameter' })
    if (node.body && ts.isBlock(node.body)) node.body.statements.forEach((s) => walk(s, decls, children))
    else if (node.body) walk(node.body, decls, children)
  } else if (ts.isSourceFile(node) || ts.isBlock(node) || ts.isCaseBlock(node)) {
    const list = ts.isCaseBlock(node) ? node.clauses : node.statements
    list.forEach((s) => walk(s, decls, children))
  } else if (ts.isCatchClause(node)) {
    if (node.variableDeclaration) for (const id of bindings(node.variableDeclaration.name)) decls.push({ id, kind: 'catch parameter' })
    walk(node.block, decls, children)
  } else {
    // for statements: the initializer and the body
    ts.forEachChild(node, (c) => walk(c, decls, children))
  }
  return { decls, children }
}

function check(file, sf, lineOffset) {
  const line = (n) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1 + lineOffset
  function visit(node, parent) {
    const { decls, children } = scopeOf(node)
    const scope = { names: new Map(), parent }
    for (const d of decls) {
      for (let s = parent; s; s = s.parent) {
        const outer = s.names.get(d.id.text)
        if (outer) {
          const l = line(d.id)
          out.push(`${file}:${l}\t${d.id.text}\t${d.kind} shadows ${outer.kind} from line ${outer.line}${outer.line > l ? ' (declared later)' : ''}`)
          break
        }
      }
      if (!scope.names.has(d.id.text)) scope.names.set(d.id.text, { kind: d.kind, line: line(d.id) })
    }
    for (const c of children) visit(c, scope)
    return scope
  }
  return visit(sf, null)
}

function files(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? files(path.join(dir, e.name)) : [path.join(dir, e.name)]))
}

// Names the template sees from the script: its top-level bindings and the props.
function templateNames(sf, top) {
  const names = new Map([...top.names].map(([k, v]) => [k, { kind: `script ${v.kind}`, line: v.line }]))
  const visitNode = (n) => {
    if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === 'defineProps') {
      const lit = n.typeArguments?.[0]
      const members = lit && ts.isTypeLiteralNode(lit) ? lit.members : n.arguments[0] && ts.isObjectLiteralExpression(n.arguments[0]) ? n.arguments[0].properties : []
      for (const m of members) if (m.name) names.set(m.name.getText(sf), { kind: 'prop', line: 0 })
    }
    ts.forEachChild(n, visitNode)
  }
  visitNode(sf)
  return names
}

// Identifiers bound by a v-for alias or slot props, parsed as arrow parameters.
function aliasNames(text) {
  const sf = ts.createSourceFile('a.ts', `(${text}) => 0`, ts.ScriptTarget.Latest, true)
  const arrow = sf.statements[0]?.expression
  if (!arrow || !ts.isArrowFunction(arrow)) return []
  return arrow.parameters.flatMap((p) => bindings(p.name)).map((id) => id.text)
}

for (const f of files(path.join(web, 'src')).sort()) {
  const rel = path.relative(path.dirname(web), f)
  const src = fs.readFileSync(f, 'utf8')
  if (f.endsWith('.ts')) {
    check(rel, ts.createSourceFile(f, src, ts.ScriptTarget.Latest, true), 0)
  } else if (f.endsWith('.vue')) {
    const { descriptor } = sfc.parse(src, { filename: f })
    let names = new Map()
    for (const block of [descriptor.script, descriptor.scriptSetup].filter(Boolean)) {
      const sf = ts.createSourceFile(f, block.content, ts.ScriptTarget.Latest, true)
      const top = check(rel, sf, block.loc.start.line - 1)
      for (const [k, v] of templateNames(sf, top)) names.set(k, v)
    }
    const walkTemplate = (node, scopes) => {
      if (node.type !== 1 && node.type !== 0) return
      let inner = scopes
      for (const p of node.props ?? []) {
        if (p.type !== 7 || !p.exp) continue
        let alias = null
        if (p.name === 'for') alias = p.exp.content.split(/\s+(?:in|of)\s+/)[0].trim().replace(/^\((.*)\)$/s, '$1')
        if (p.name === 'slot') alias = p.exp.content
        if (alias === null) continue
        const own = new Map()
        for (const n of aliasNames(alias)) {
          for (const s of inner) {
            if (s.has(n)) {
              const o = s.get(n)
              out.push(`${rel}:${p.loc.start.line}\t${n}\ttemplate v-${p.name} shadows ${o.kind}${o.line ? ` from line ${o.line}` : ''}`)
              break
            }
          }
          own.set(n, { kind: `template v-${p.name}`, line: p.loc.start.line })
        }
        inner = [own, ...inner]
      }
      for (const c of node.children ?? []) walkTemplate(c, inner)
    }
    if (descriptor.template?.ast) walkTemplate(descriptor.template.ast, [names])
  }
}
if (out.length) {
  console.log(out.join('\n'))
  process.exit(1)
}
