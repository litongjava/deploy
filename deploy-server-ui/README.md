# tio-boot-admin-react
## common file
src/components/common
src/services/system/systemService.ts
src/utils/apiTable.ts
src/utils/downloadUtils.ts
src/utils/mongodbToJson.ts

## Demo
- https://litongjava.github.io/tio-boot-admin-react/
- admin/ant.design
## Environment Prepare

Install `node_modules`:

```bash
pnpm install
```

### Node 版本（已随 umi 4.7.22 从 22 升到 25.2.1）

历史坑（已修复，留作背景）：umi **4.4.x** 的依赖链 `spdy@4.0.2 → http-deceiver@1.2.7` 在
`require` 时调用 Node 私有 API `process.binding('http_parser')`，它自 **Node.js 24 起被移除**（DEP0111）。
于是 Node >= 24 上（服务器用 `install.sh --lts` 装的默认 LTS 恰好就是 24+）`pnpm install` 的
postinstall（`max setup`）会直接 fatal：

```txt
fatal - Error: Register plugin .../preset-umi/dist/features/... failed
  [cause]: Error: No such module: http_parser
```

**现状**：`@umijs/max` 已升级到 **4.7.22**，其 `@umijs/bundler-utils >= 4.6.30` 在 Node >= 24 时
不再 `require('spdy')`，改用内置 `https.createServer`（上游
[umijs/umi#13256](https://github.com/umijs/umi/pull/13256)），所以 **Node 22 与 Node 25 都能构建**。
本目录已在 Node 25.2.1 上实测：`pnpm install`（含 `max setup`）、`pnpm build`、`pnpm start:dev` 全部通过，
并且同一份 Node 25.2.1 下 `require('http-deceiver')` 仍会报 `No such module: http_parser`
—— 说明崩溃源还在，只是 umi 4.7.22 不再去加载它。

`.npmrc` 把 pnpm 的运行时钉在 **25.2.1**，与机器默认版本无关：

- `use-node-version=25.2.1`：`pnpm install` / `pnpm run` 一律用它执行（首次从 npmmirror 下载一份 Node，之后走缓存）
- `engine-strict=true`：版本不匹配时（例如系统 node 下直接 `npm install`）抛明确的引擎报错；`engines` 同时放宽为 `>=22 <26`

不想让 pnpm 自己下载 Node 时，手动兜底：

```bash
nvm install 25.2.1 && nvm alias default 25.2.1
rm -rf node_modules && pnpm install
```

### husky / `prepare`（装了 Node 22 之后的下一个坑）

`prepare` 原本是 `husky install`。husky 7 要求 cwd **就是 git 根目录**，而本包位于仓库根
（`deploy/`）的子目录，于是它必然抛 `.git can't be found` 并以 1 退出，把
`pnpm install && pnpm build` 整条链断掉（postinstall 修好后就会撞到它）。现按 husky 文档的
子目录写法改成从仓库根执行：

```json
"prepare": "cd .. && husky install deploy-server-ui/.husky"
```

它会把 `core.hooksPath` 指到 `deploy-server-ui/.husky`。注意该目录目前**没有任何 hook 文件**
（只有 husky 运行时的 `_/husky.sh`），也就是说不装 hook 也不影响任何功能。

## Provided Scripts

Tio Boot Admin provides some useful script to help you quick start and build with web project, code style check and test.

Scripts provided in `package.json`. It's safe to modify or add additional script:

### Start project

```bash
pnpm start
```

or

```
pnpm dev
```

### Build project

```bash
pnpm build
```

### Check code style

```bash
pnpm lint
```

You can also use script to auto fix some lint error:

```bash
pnpm lint:fix
```

### Test code

```bash
pnpm test
```
### deploy to github gh-pages
```bash
pnpm build
pnpm gh-pages
```
### flyio
```
fly apps create kimi-admin-react
fly deploy
```
## dev
.env
```env
TIO_BOOT_ADMIN_BACKEND_URL=http://localhost:8100
```
## More

You can view full document on our [official website](https://pro.ant.design). And welcome any feedback in our [github](https://github.com/ant-design/ant-design-pro).
