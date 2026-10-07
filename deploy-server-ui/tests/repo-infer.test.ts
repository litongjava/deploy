import {
  inferProjectFromGitUrl,
  projectKeyFromRepoName,
  repoNameFromGitUrl,
} from '../src/utils/repoInfer';

/**
 * 从 Git 地址推断项目 Key / 名称。
 *
 * 这组用例覆盖的是「用户第一件事就是粘仓库地址」的场景：地址形态五花八门
 * （https / ssh / SCP 简写 / 相对路径 / 带查询串），推错了会直接落到
 * 工作目录与 Key 上，改起来麻烦，所以每种形态都钉一条。
 */
describe('repoNameFromGitUrl', () => {
  it('从 https 地址取仓库名', () => {
    expect(repoNameFromGitUrl('https://gitee.com/ppnt/tio-boot-web-hello.git')).toBe(
      'tio-boot-web-hello',
    );
    expect(repoNameFromGitUrl('https://github.com/owner/my-app')).toBe('my-app');
    expect(repoNameFromGitUrl('http://git.internal:8080/group/sub/repo.git')).toBe('repo');
  });

  it('从 SCP 简写（git@host:owner/repo）取仓库名', () => {
    expect(repoNameFromGitUrl('git@github.com:xxx/yyy.git')).toBe('yyy');
    expect(repoNameFromGitUrl('git@gitee.com:ppnt/deploy.git')).toBe('deploy');
  });

  it('从 ssh:// 地址取仓库名', () => {
    expect(repoNameFromGitUrl('ssh://git@github.com/owner/repo.git')).toBe('repo');
  });

  it('去掉结尾斜杠、查询串与 .git 后缀', () => {
    expect(repoNameFromGitUrl('https://gitee.com/ppnt/deploy.git/')).toBe('deploy');
    expect(repoNameFromGitUrl('https://github.com/owner/repo?ref=main')).toBe('repo');
    expect(repoNameFromGitUrl('https://github.com/owner/repo#readme')).toBe('repo');
  });

  it('支持 owner/repo 这种相对路径', () => {
    expect(repoNameFromGitUrl('ppnt/deploy')).toBe('deploy');
    expect(repoNameFromGitUrl('deploy')).toBe('deploy');
  });

  it('中文仓库名按 URL 解码后再用', () => {
    expect(repoNameFromGitUrl('https://gitee.com/owner/%E6%B5%8B%E8%AF%95.git')).toBe('测试');
  });

  it('推不出来时返回 null', () => {
    expect(repoNameFromGitUrl('')).toBeNull();
    expect(repoNameFromGitUrl('   ')).toBeNull();
    expect(repoNameFromGitUrl(undefined)).toBeNull();
    // 只有主机名、没有路径：推不出仓库名
    expect(repoNameFromGitUrl('https://github.com')).toBeNull();
    expect(repoNameFromGitUrl('https://github.com/')).toBeNull();
  });

  it('路径只有一段时也当仓库名（自建 Git 常见 /repo.git）', () => {
    expect(repoNameFromGitUrl('https://git.internal/repo.git')).toBe('repo');
    expect(repoNameFromGitUrl('https://github.com/owner/')).toBe('owner');
  });
});

describe('projectKeyFromRepoName', () => {
  it('保留合法字符与大小写', () => {
    expect(projectKeyFromRepoName('tio-boot-web-hello')).toBe('tio-boot-web-hello');
    expect(projectKeyFromRepoName('My_App2')).toBe('My_App2');
  });

  it('把非法字符换成 - 并合并连续符号', () => {
    expect(projectKeyFromRepoName('my app')).toBe('my-app');
    expect(projectKeyFromRepoName('my  app')).toBe('my-app');
    expect(projectKeyFromRepoName('repo.git')).toBe('repo-git');
    expect(projectKeyFromRepoName('测试项目')).toBe('');
  });

  it('首尾不能是符号（与表单校验一致）', () => {
    expect(projectKeyFromRepoName('-abc-')).toBe('abc');
    expect(projectKeyFromRepoName('.hidden')).toBe('hidden');
    expect(projectKeyFromRepoName('__x__')).toBe('x');
  });

  it('超长时截断到 64 位且不以符号结尾', () => {
    const long = 'a'.repeat(70);
    expect(projectKeyFromRepoName(long)).toHaveLength(64);
    const longWithTail = `${'a'.repeat(63)}-bcd`;
    const out = projectKeyFromRepoName(longWithTail);
    expect(out.length).toBeLessThanOrEqual(64);
    expect(out).toMatch(/[a-zA-Z0-9]$/);
  });

  it('结果必须能通过表单里的 Key 正则', () => {
    const pattern = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/;
    for (const name of ['my app', '-x-', 'a.b.c', 'v1.2.3', '团队-仓库']) {
      const key = projectKeyFromRepoName(name);
      if (key) expect(key).toMatch(pattern);
    }
  });
});

describe('inferProjectFromGitUrl', () => {
  it('一次给出 Key 与名称', () => {
    expect(inferProjectFromGitUrl('https://gitee.com/ppnt/tio-boot-web-hello.git')).toEqual({
      key: 'tio-boot-web-hello',
      name: 'tio-boot-web-hello',
    });
  });

  it('名称保留原文，Key 用规整后的结果', () => {
    expect(inferProjectFromGitUrl('https://github.com/owner/my%20app.git')).toEqual({
      key: 'my-app',
      name: 'my app',
    });
  });

  it('推不出 Key 时整体返回 null（而不是给个空 Key）', () => {
    expect(inferProjectFromGitUrl('https://github.com/owner/测试')).toBeNull();
  });
});