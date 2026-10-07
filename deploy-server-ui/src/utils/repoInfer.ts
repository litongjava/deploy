/**
 * 从 Git 仓库地址推断项目 Key / 名称。
 *
 * 新建项目页把「Git 准备阶段」放在第一张卡片，用户第一件事就是填仓库地址；
 * 地址里其实已经带着仓库名，所以 Key / 名称 / 工作目录都据此预填，省掉重复输入。
 * 三个字段都遵守同一条规则：**用户一旦自己改过就不再覆盖**（见编辑页的 *EditedRef）。
 *
 * 这里是纯函数，不碰表单；编辑页负责把结果写进表单。
 */

/** 支持的地址形态：https / http / ssh 的 URL，`git@host:owner/repo` 这种 SCP 简写，以及 owner/repo 相对路径 */
export function repoNameFromGitUrl(raw?: string | null): string | null {
  const trimmed = (raw ?? '').trim();
  if (!trimmed) return null;

  // 去掉查询串与片段（`?ref=main`、`#readme`），再去掉结尾的斜杠
  let s = trimmed.split(/[?#]/)[0].replace(/\/+$/, '');

  let path: string;
  const scheme = s.match(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//);
  if (scheme) {
    // 带 scheme：跳过 `scheme://`，再跳过主机名（含可能的 user@host:port），剩下的才是路径
    const rest = s.slice(scheme[0].length);
    const slash = rest.indexOf('/');
    // 只有主机名、没有路径（`https://github.com`）→ 推不出仓库名
    if (slash === -1) return null;
    path = rest.slice(slash + 1);
  } else if (/^[^/@]+@[^/:]+:/.test(s)) {
    // SCP 简写 `git@github.com:owner/repo.git`：冒号后面就是路径
    path = s.slice(s.indexOf(':') + 1);
  } else {
    // 相对路径或本地目录 `owner/repo`
    path = s;
  }

  const segments = path.split('/').filter(Boolean);
  if (segments.length === 0) return null;

  let name = segments[segments.length - 1].replace(/\.git$/i, '').trim();
  try {
    name = decodeURIComponent(name);
  } catch (e) {
    // 不是合法的百分号编码：按原样用
  }
  return name || null;
}

/**
 * 仓库名 → 合法的项目 Key。规则与表单校验保持一致（`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`）：
 * 非法字符换成 `-`、首尾不能是符号、最长 64。
 *
 * 刻意**不动大小写**：Key 会出现在工作目录路径里，Linux 区分大小写，
 * 擅自改成小写等于换了个目录，反而更难对账。
 */
export function projectKeyFromRepoName(name: string): string {
  let key = (name ?? '').replace(/[^a-zA-Z0-9_-]+/g, '-');
  key = key.replace(/^[^a-zA-Z0-9]+/, '').replace(/[^a-zA-Z0-9]+$/, '');
  key = key.replace(/-{2,}/g, '-');
  if (key.length > 64) {
    key = key.slice(0, 64).replace(/[^a-zA-Z0-9]+$/, '');
  }
  return key;
}

export interface GitUrlInference {
  /** 仓库名，用作项目名称 */
  name: string;
  /** 由仓库名规整出的项目 Key */
  key: string;
}

/** 从地址一次性推出 Key 与名称；推不出来（地址为空 / 不合法）时返回 null */
export function inferProjectFromGitUrl(raw?: string | null): GitUrlInference | null {
  const repo = repoNameFromGitUrl(raw);
  if (!repo) return null;
  const key = projectKeyFromRepoName(repo);
  if (!key) return null;
  return { name: repo, key };
}