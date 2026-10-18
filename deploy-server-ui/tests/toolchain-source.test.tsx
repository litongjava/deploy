import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { Modal } from 'antd';
import ToolchainPanel from '../src/components/ToolchainPanel';
import { systemApi } from '../src/services/system';

jest.mock('@umijs/max', () => ({ useAccess: () => ({ canAdmin: true }), history: { push: jest.fn() } }));
jest.mock('../src/i18n', () => ({ t: (s: string, values: Record<string, unknown> = {}) => s.replace(/\{(\w+)\}/g, (_, k) => String(values[k])) }));
jest.mock('../src/services/run', () => ({ runApi: { list: jest.fn().mockResolvedValue({ list: [] }) } }));
jest.mock('../src/services/system', () => ({ systemApi: { toolchains: jest.fn(), installToolchains: jest.fn() } }));

// Umi's Jest transformer emits classic JSX; expose React for transformed components.
(globalThis as any).React = React;
let dialog: any;
beforeEach(() => {
  jest.clearAllMocks();
  (systemApi.toolchains as jest.Mock).mockResolvedValue({ supported: true, missing: ['go'], components: [
    { key: 'go', name: 'Go', installed: false, fields: [] },
    { key: 'rust', name: 'Rust', installed: true, fields: [] },
  ] });
  (systemApi.installToolchains as jest.Mock).mockResolvedValue({ runId: 12 });
  jest.spyOn(Modal, 'confirm').mockImplementation((config) => { dialog = config; return { destroy: jest.fn(), update: jest.fn() }; });
});
afterEach(() => jest.restoreAllMocks());

it.each(['安装', '重装', '一键安装缺失（1）', '安装所选（1）', '预演'])('%s defaults to official without submitting before confirmation', async (label) => {
  render(<ToolchainPanel />);
  await screen.findByText('Go');
  fireEvent.click(screen.getByText(label));
  expect(systemApi.installToolchains).not.toHaveBeenCalled();
  render(dialog.content);
  expect((screen.getByLabelText('官方源（适合国外服务器）') as HTMLInputElement).checked).toBe(true);
  await act(async () => { await dialog.onOk(); });
  expect(systemApi.installToolchains).toHaveBeenCalledWith(expect.objectContaining({ mirror: 'off', dryRun: label === '预演' }));
});

it('submits China mirrors, keeps failures retryable, and resets on reopen', async () => {
  render(<ToolchainPanel />);
  await screen.findByText('Go');
  fireEvent.click(screen.getByText('安装'));
  const content = render(dialog.content);
  fireEvent.click(screen.getByLabelText('国内镜像（适合国内服务器）'));
  (systemApi.installToolchains as jest.Mock).mockRejectedValueOnce(new Error('network'));
  await act(async () => { await expect(dialog.onOk()).rejects.toThrow('network'); });
  await act(async () => { await dialog.onOk(); });
  expect(systemApi.installToolchains).toHaveBeenLastCalledWith(expect.objectContaining({ mirror: 'cn' }));
  content.unmount();
  fireEvent.click(screen.getByText('安装'));
  render(dialog.content);
  expect((screen.getByLabelText('官方源（适合国外服务器）') as HTMLInputElement).checked).toBe(true);
});
