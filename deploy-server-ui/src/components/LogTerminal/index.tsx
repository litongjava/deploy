import { getToken } from '@/services/authStore';
import type { WsFrame } from '@/services/types';
import { API_BASE } from '@/services/api';
import { SearchAddon } from '@xterm/addon-search';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import {
  ClearOutlined,
  DownloadOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  SearchOutlined,
  VerticalAlignBottomOutlined,
} from '@ant-design/icons';
import { Badge, Button, Input, Space, Tooltip } from 'antd';
import React, { useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react';
import '@xterm/xterm/css/xterm.css';

export interface LogTerminalHandle {
  clear: () => void;
  scrollToBottom: () => void;
  write: (text: string) => void;
  /**
   * 按 offset 写入一段日志：与已经落屏的内容按 offset 去重。
   * 用于「HTTP 补历史」与「WebSocket history 帧」两条路径同时存在时的去重。
   */
  writeAt: (offset: number, text: string) => void;
}

interface LogTerminalProps {
  runId: number | string;
  /** 是否仍然活跃（running/queued）。结束时不再重连。 */
  active: boolean;
  onFrame?: (frame: WsFrame) => void;
  onExit?: (status: string, exitCode: number | null) => void;
  onError?: (msg: string) => void;
  /** 下载按钮点击（由外部实现，因为需要 Authorization 头） */
  onDownload?: () => void;
  height?: number;
}

const MAX_BACKOFF = 10000;

type ConnectionState = 'connecting' | 'open' | 'reconnecting' | 'closed';

/**
 * 运行日志实时终端。
 *
 * - WebSocket 地址：/api/v1/ws/runs/{id}?token=<jwt>&offset=<n>
 *   （浏览器 WS 不能自定义 Header，token 只能走 query，见 API 文档 §7.4）
 * - history / log 帧原样写入终端，保留 ANSI 转义
 * - 断线用收到的 nextOffset 作为下次连接 offset，指数退避重连（1s→10s），补齐缺失日志
 * - 用户手动向上滚动时暂停自动滚动，滚动回底部自动恢复
 */
const LogTerminal = React.forwardRef<LogTerminalHandle, LogTerminalProps>(
  ({ runId, active, onFrame, onExit, onError, onDownload, height = 520 }, ref) => {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const termRef = useRef<Terminal | null>(null);
    const fitRef = useRef<FitAddon | null>(null);
    const searchRef = useRef<SearchAddon | null>(null);
    const wsRef = useRef<WebSocket | null>(null);
    const offsetRef = useRef<number>(0);
    const closedRef = useRef<boolean>(false);
    const retryRef = useRef<number>(0);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const autoScrollRef = useRef<boolean>(true);
    const finishedRef = useRef<boolean>(!active);
    // 已经落屏的字节数：重连时若服务端片段与本地有重叠，按长度裁掉重复部分
    const streamedRef = useRef<number>(0);
    const scheduleReconnectRef = useRef<() => void>(() => undefined);
    // 终端实例是否还在挂载状态（React 18 StrictMode 会卸载再挂载，写已销毁的终端会抛错）
    const aliveRef = useRef<boolean>(false);

    const [autoScroll, setAutoScroll] = useState(true);
    const [state, setState] = useState<ConnectionState>('connecting');
    const [searchOpen, setSearchOpen] = useState(false);
    const [searchTerm, setSearchTerm] = useState('');

    /** 原样写入 + 处理进度条用的 \r（只保留最后一段） */
    const writeData = useCallback((text: string) => {
      const term = termRef.current;
      if (!term || !text || !aliveRef.current) return;
      try {
        // 服务端推的是原始字节片段，可能含 \r 覆盖语义（进度条）。
        // 只在整段没有换行、且是 \r 覆盖时保留最后一段，其余原样写。
        if (text.indexOf('\r') >= 0 && text.indexOf('\n') === -1) {
          const last = text.slice(text.lastIndexOf('\r') + 1);
          term.write(`\r${last}`);
        } else {
          term.write(text);
        }
        if (autoScrollRef.current) {
          term.scrollToBottom();
        }
      } catch (e) {
        // 实例已被销毁，忽略
      }
    }, []);

    /**
     * 按 offset 写入一段日志。
     * 与 liveStreamedRef（已落屏字节数）比较：整段已落屏就丢弃，部分重叠就裁掉重复前缀。
     * 这样「HTTP 补历史」与「WebSocket history 帧」先后到达都不会重复刷屏。
     */
    const writeAt = useCallback(
      (offset: number, text: string) => {
        if (!text) return;
        let data = text;
        const already = streamedRef.current;
        if (offset < already) {
          data = text.slice(already - offset);
        }
        if (!data) return;
        writeData(data);
        if (offset + text.length > streamedRef.current) {
          streamedRef.current = offset + text.length;
        }
      },
      [writeData],
    );

    const clear = useCallback(() => {
      termRef.current?.clear();
      streamedRef.current = offsetRef.current;
    }, []);

    useImperativeHandle(ref, () => ({
      clear,
      scrollToBottom: () => {
        setAutoScroll(true);
        autoScrollRef.current = true;
        termRef.current?.scrollToBottom();
      },
      write: writeData,
      writeAt,
    }));

    /** 处理服务端帧 */
    const handleFrame = useCallback(
      (frame: WsFrame) => {
        onFrame?.(frame);
        switch (frame.type) {
          case 'history':
          case 'log': {
            let data = frame.data || '';
            const nextOffset =
              typeof frame.nextOffset === 'number' ? frame.nextOffset : undefined;
            if (data) {
              // 按 offset 去重：WebSocket 会从 offset 续传，但「HTTP 补历史」可能已经
              // 先写入了同样的内容，这里统一走 writeAt 裁掉重复部分。
              const start =
                nextOffset !== undefined ? nextOffset - data.length : streamedRef.current;
              writeAt(start, data);
            }
            if (nextOffset !== undefined) {
              offsetRef.current = nextOffset;
            }
            break;
          }
          case 'status':
            break;
          case 'exit': {
            finishedRef.current = true;
            setState('closed');
            onExit?.(frame.status, frame.exitCode ?? null);
            break;
          }
          case 'error':
            onError?.(frame.message || '未知错误');
            break;
          default:
            break;
        }
      },
      [onExit, onError, onFrame, writeAt],
    );

    /** 建立连接 */
    const connect = useCallback(() => {
      const term = termRef.current;
      if (!term) return;
      if (finishedRef.current) {
        setState('closed');
        return;
      }
      const token = getToken();
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = `${proto}//${window.location.host}${API_BASE}/ws/runs/${runId}?token=${encodeURIComponent(
        token,
      )}&offset=${offsetRef.current}`;

      setState((s) => (s === 'connecting' ? s : 'reconnecting'));
      let ws: WebSocket;
      try {
        ws = new WebSocket(url);
      } catch (e: any) {
        onError?.(`建立 WebSocket 失败：${e?.message || e}`);
        scheduleReconnectRef.current();
        return;
      }
      wsRef.current = ws;

      ws.onopen = () => {
        retryRef.current = 0;
        setState('open');
      };

      ws.onmessage = (ev) => {
        if (typeof ev.data !== 'string') return;
        try {
          const frame = JSON.parse(ev.data) as WsFrame;
          handleFrame(frame);
        } catch (e) {
          // 非 JSON 帧直接当日志处理
          writeData(String(ev.data));
        }
      };

      ws.onerror = () => {
        // onclose 会紧随其后，重连逻辑统一放那里
      };

      ws.onclose = () => {
        wsRef.current = null;
        if (closedRef.current || finishedRef.current) {
          setState('closed');
          return;
        }
        scheduleReconnectRef.current();
      };
    }, [handleFrame, onError, runId, writeData]);

    /** 指数退避重连：1s/2s/4s/8s…最多 10s */
    const scheduleReconnect = useCallback(() => {
      if (closedRef.current || finishedRef.current) return;
      const attempt = retryRef.current;
      const delay = Math.min(1000 * 2 ** attempt, MAX_BACKOFF);
      retryRef.current = attempt + 1;
      setState('reconnecting');
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        connect();
      }, delay);
    }, [connect]);

    // 让 connect 能拿到最新的 scheduleReconnect（避免函数声明顺序问题）
    scheduleReconnectRef.current = scheduleReconnect;

    // 初始化终端
    useEffect(() => {
      const container = containerRef.current;
      if (!container) return;

      const term = new Terminal({
        // convertEol 必须为 true：日志文件是按 `\n`（LF）写的，而 xterm 默认的
        // convertEol:false 会让 LF 只下移一行、**不回列首** —— 结果是每一行都接在
        // 上一行末尾继续画，出现"阶梯"式错位，长行还会在终端宽度处被从中间切断
        // （例如 `Started : 2026-10` 换行成 `-04T20:21:21+08:00`）。
        // 打开后 LF 等价于 CRLF，行首对齐；`\r` 进度条语义不受影响。
        convertEol: true,
        cursorBlink: false,
        disableStdin: true,
        scrollback: 20000,
        fontSize: 13,
        fontFamily: 'Menlo, Monaco, Consolas, "Courier New", monospace',
        theme: {
          background: '#1e1e1e',
          foreground: '#d4d4d4',
          cursor: '#d4d4d4',
        },
      });
      const fit = new FitAddon();
      const search = new SearchAddon();
      term.loadAddon(fit);
      term.loadAddon(search);
      term.open(container);
      termRef.current = term;
      fitRef.current = fit;
      searchRef.current = search;
      aliveRef.current = true;
      // 重新挂载时把连接与「已落屏」状态一起重置
      // （StrictMode 会卸载再挂载，若只清终端内容而留着 streamedRef，
      //  重连时会把本该显示的历史日志当成重复内容裁掉）
      closedRef.current = false;
      finishedRef.current = !active;
      offsetRef.current = 0;
      streamedRef.current = 0;
      retryRef.current = 0;
      try {
        fit.fit();
      } catch (e) {
        // ignore
      }

      // 用户滚动时暂停/恢复自动滚动
      const scrollSub = term.onScroll(() => {
        const buffer = term.buffer?.active;
        if (!buffer) return;
        const atBottom = buffer.viewportY >= buffer.baseY - 1;
        if (atBottom !== autoScrollRef.current) {
          autoScrollRef.current = atBottom;
          setAutoScroll(atBottom);
        }
      });

      const onResize = () => {
        try {
          fit.fit();
        } catch (e) {
          // ignore
        }
      };
      window.addEventListener('resize', onResize);
      const ro =
        typeof ResizeObserver !== 'undefined'
          ? new ResizeObserver(() => onResize())
          : null;
      ro?.observe(container);

      connect();

      return () => {
        closedRef.current = true;
        aliveRef.current = false;
        if (timerRef.current) clearTimeout(timerRef.current);
        window.removeEventListener('resize', onResize);
        ro?.disconnect();
        scrollSub.dispose();
        try {
          wsRef.current?.close();
        } catch (e) {
          // ignore
        }
        wsRef.current = null;
        term.dispose();
        termRef.current = null;
        fitRef.current = null;
        searchRef.current = null;
      };
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // 运行结束后主动断开（服务端补完 history 会立刻发 exit 并关闭连接，这里兜底）
    useEffect(() => {
      if (!active) {
        finishedRef.current = true;
        setState((s) => (s === 'closed' ? s : 'closed'));
        if (timerRef.current) clearTimeout(timerRef.current);
        try {
          wsRef.current?.close();
        } catch (e) {
          // ignore
        }
      }
    }, [active]);

    const stateMeta: Record<ConnectionState, { color: string; text: string }> = {
      connecting: { color: 'processing', text: '连接中' },
      open: { color: 'success', text: '已连接' },
      reconnecting: { color: 'warning', text: '断线重连中' },
      closed: { color: 'default', text: active ? '已断开' : '已结束' },
    };

    return (
      <div>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            flexWrap: 'wrap',
            gap: 8,
            marginBottom: 8,
          }}
        >
          <Space wrap>
            <Badge
              status={stateMeta[state].color as any}
              text={`日志流：${stateMeta[state].text}`}
            />
            {!autoScroll ? (
              <Tooltip title="你手动向上滚动了，新日志不会自动跟随">
                <span style={{ color: '#faad14' }}>已暂停自动滚动</span>
              </Tooltip>
            ) : (
              <span style={{ color: '#999' }}>自动滚动中</span>
            )}
          </Space>
          <Space wrap>
            <Button
              size="small"
              icon={autoScroll ? <PauseCircleOutlined /> : <PlayCircleOutlined />}
              onClick={() => {
                const next = !autoScroll;
                setAutoScroll(next);
                autoScrollRef.current = next;
                if (next) termRef.current?.scrollToBottom();
              }}
            >
              {autoScroll ? '暂停滚动' : '恢复滚动'}
            </Button>
            <Button
              size="small"
              icon={<VerticalAlignBottomOutlined />}
              onClick={() => {
                setAutoScroll(true);
                autoScrollRef.current = true;
                termRef.current?.scrollToBottom();
              }}
            >
              滚到底部
            </Button>
            <Button size="small" icon={<ClearOutlined />} onClick={clear}>
              清屏
            </Button>
            <Button
              size="small"
              icon={<SearchOutlined />}
              onClick={() => setSearchOpen((v) => !v)}
            >
              搜索
            </Button>
            <Button
              size="small"
              icon={<DownloadOutlined />}
              onClick={() => onDownload?.()}
            >
              下载日志
            </Button>
          </Space>
        </div>

        {searchOpen ? (
          <Space style={{ marginBottom: 8 }}>
            <Input
              size="small"
              allowClear
              placeholder="搜索日志内容，回车下一个"
              value={searchTerm}
              style={{ width: 280 }}
              onChange={(e) => setSearchTerm(e.target.value)}
              onPressEnter={() => searchRef.current?.findNext(searchTerm)}
            />
            <Button size="small" onClick={() => searchRef.current?.findNext(searchTerm)}>
              下一个
            </Button>
            <Button size="small" onClick={() => searchRef.current?.findPrevious(searchTerm)}>
              上一个
            </Button>
            <Button
              size="small"
              onClick={() => {
                searchRef.current?.clearDecorations();
                setSearchTerm('');
              }}
            >
              清除
            </Button>
          </Space>
        ) : null}

        <div
          ref={containerRef}
          style={{
            height,
            width: '100%',
            background: '#1e1e1e',
            padding: 8,
            borderRadius: 4,
            overflow: 'hidden',
          }}
        />
      </div>
    );
  },
);

LogTerminal.displayName = 'LogTerminal';

export default LogTerminal;
