import type { ProjectKind } from '@/services/types';
import { PROJECT_KIND_META } from '@/utils/constants';
import { AppstoreOutlined, BuildOutlined } from '@ant-design/icons';
import { Tooltip } from 'antd';
import React from 'react';

/**
 * 项目类型图标。
 *
 * 列表里原来把「应用项目 / 构建项目」整段文字当标签显示，一屏下来全是重复的四个字，
 * 既占宽度又不好扫读；这里换成图标 + 悬浮提示：形状一眼区分类型，文字只在需要时出现。
 * 颜色取自 PROJECT_KIND_META.iconColor，与项目详情页里的 Tag 保持同一套语义色。
 */
const KIND_ICONS: Record<ProjectKind, React.ReactNode> = {
  // 图标本身带 aria-label="appstore" 这类可访问名，会盖掉外层的中文含义，
  // 所以这里把它从可访问性树里摘掉，只留外层 span 的 aria-label。
  app: <AppstoreOutlined aria-hidden />,
  lib: <BuildOutlined aria-hidden />,
};

export type ProjectKindIconProps = {
  /** 项目类型；后端异常值按应用项目兜底，避免整行渲染不出来 */
  kind?: ProjectKind;
  style?: React.CSSProperties;
};

const ProjectKindIcon: React.FC<ProjectKindIconProps> = ({ kind, style }) => {
  const key = (kind || 'app') as ProjectKind;
  const meta = PROJECT_KIND_META[key] || PROJECT_KIND_META.app;
  return (
    <Tooltip title={meta.text}>
      <span
        role="img"
        aria-label={meta.text}
        style={{
          color: meta.iconColor,
          fontSize: 15,
          lineHeight: 1,
          verticalAlign: 'middle',
          ...style,
        }}
      >
        {KIND_ICONS[key] || KIND_ICONS.app}
      </span>
    </Tooltip>
  );
};

export default ProjectKindIcon;
