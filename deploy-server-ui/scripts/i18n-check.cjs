/**
 * 从代码里抽出所有 t('...') 的 key，与 en-US.ts 比对，报告缺失/多余的词条。
 * 用法：node scripts/i18n-check.cjs
 *
 * 退出码：0 = 词条齐全；1 = 有缺失（可用于 CI 卡口）。
 */
const ts = require('typescript');
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..', 'src');
const SKIP = new Set(['node_modules', '.umi', '.umi-production', '.umi-test', 'locales', 'i18n']);

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) { if (!SKIP.has(e.name)) walk(p, out); }
    else if (/\.tsx?$/.test(e.name)) out.push(p);
  }
  return out;
}

const keys = new Map(); // key -> {count, locs}
for (const file of walk(ROOT)) {
  const rel = path.relative(process.cwd(), file).replace(/\\/g, '/');
  const text = fs.readFileSync(file, 'utf8');
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  function visit(node) {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 't') {
      const arg = node.arguments[0];
      if (arg && (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg))) {
        const k = arg.text;
        if (!keys.has(k)) keys.set(k, { count: 0, locs: [] });
        const rec = keys.get(k);
        rec.count++;
        const lc = sf.getLineAndCharacterOfPosition(arg.getStart());
        if (rec.locs.length < 3) rec.locs.push(`${rel}:${lc.line + 1}`);
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(sf);
}

// 读 en-US.ts（用 TS 编译取默认导出）
const enPath = path.join(ROOT, 'locales', 'en-US.ts');
const enSrc = fs.readFileSync(enPath, 'utf8');
const out = ts.transpileModule(enSrc, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2019 } });
const mod = { exports: {} };
new Function('exports', 'module', 'require', out.outputText)(mod.exports, mod, require);
const en = mod.exports.default;

const zhPath = path.join(ROOT, 'locales', 'zh-CN.ts');
const zhOut = ts.transpileModule(fs.readFileSync(zhPath, 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2019 } });
const zhMod = { exports: {} };
new Function('exports', 'module', 'require', zhOut.outputText)(zhMod.exports, zhMod, require);
const zh = zhMod.exports.default;

const missing = [...keys.keys()].filter((k) => !(k in en)).sort((a, b) => a.localeCompare(b, 'zh'));
const unused = Object.keys(en).filter((k) => !keys.has(k)).sort((a, b) => a.localeCompare(b, 'zh'));

console.log(`t() keys in code: ${keys.size}`);
console.log(`en-US entries:    ${Object.keys(en).length}`);
console.log(`zh-CN entries:    ${Object.keys(zh).length}`);
console.log(`\nMISSING in en-US: ${missing.length}`);
missing.forEach((k) => console.log(`   ${JSON.stringify(k)}   // ${keys.get(k).locs[0]}`));
console.log(`\nUNUSED en-US keys: ${unused.length}`);
unused.forEach((k) => console.log(`   ${JSON.stringify(k)}`));

if (process.argv.includes('--emit-missing')) {
  fs.writeFileSync('.i18n-missing.json', JSON.stringify(missing, null, 2), 'utf8');
  console.log('\nwrote .i18n-missing.json');
}