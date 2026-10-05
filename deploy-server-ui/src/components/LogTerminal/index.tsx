import { getToken } from '@/services/authStore';
import type { WsFrame } from '@/services/types';
import { API_BASE } from '@/services/api';
import { isRunFinished } from '@/utils/constants';
import { SearchAddon } from '@xterm/addon-search';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import {
  ClearOutlined,
  DownloadOutlined,
  LinkOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
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
  /** 手动重连：清掉退避计数，立刻从当前 offset 继续（运行已结束也能用它补回日志） */
  reconnect: () => void;
  /** 重载日志：清空终端与去重状态，从 offset 0 重新拉一遍（终端空白/错乱时用） */
  reload: () => void;
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
/** 运行结束后（补尾巴阶段）允许的重连次数，避免日志被清理后无限重连 */
const MAX_OVER_RETRY = 6;
/** 服务端说「本连接被截断」后的重连延迟：要快，但不能变成忙等 */
const TRUNCATED_RETRY_MS = 250;

/**
 * 日志偏移是**字节**，而 JS 的 string.length 是 UTF-16 码元数。
 * 日志里只要有中文/emoji，两者就对不上，按 string 下标裁剪就会裁错位置
 * （把本该显示的内容当成重复内容截掉，表现是日志缺行）。
 * 所以统一用 TextEncoder/TextDecoder 做字节对齐。
 */
const utf8Encoder = new TextEncoder();
const utf8Decoder = new TextDecoder();
const byteLength = (s: string) => utf8Encoder.encode(s).length;

type ConnectionState = 'connecting' | 'open' | 'reconnecting' | 'closed';

/**
 * 运行日志实时终端。
 *
 * - WebSocket 地址：/api/v1/ws/runs/{id}?token=<jwt>&offset=<n>
 *   （浏览器 WS 不能自定义 Header，token 只能走 query，见 API 文档 §7.4）
 * - history / log 帧原样写入终端，保留 ANSI 转义
 * - 断线用收到的 nextOffset 作为下次连接 offset，指数退避重连（1s→10s），补齐缺失日志
 * - **只有拿到「终态 exit 帧」才算结束**：exit 的 status 不是终态时说明服务端
 *   只是把跟不上的订阅者踢掉了（truncated 帧），必须继续重连补齐，否则日志会永久缺尾
 * - 运行结束（active=false）时不主动断开：连接还在就让它自然收尾，
 *   连接已断则再连一次按 offset 取回尾巴
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
    /** 运行是否已处于终态（来自 active=false 或 exit 帧） */
    const runOverRef = useRef<boolean>(!active);
    /** 是否已经收到「终态 exit 帧」：这才是真正的收工信号，之后不再重连 */
    const doneRef = useRef<boolean>(false);
    /** 本实例是否在运行期间连过：决定运行结束后要不要再连一次补尾巴 */
    const liveRef = useRef<boolean>(active);
    /** 运行结束后的补收次数上限，避免日志被清理后无限重连 */
    const overRetryRef = useRef<number>(0);
    // 已经落屏的字节数：重连时若服务端片段与本地有重叠，按字节裁掉重复部分
    const streamedRef = useRef<number>(0);
    const scheduleReconnectRef = useRef<() => void>(() => undefined);
    const reconnectSoonRef = useRef<() => void>(() => undefined);
    const connectRef = useRef<(force?: boolean) => void>(() => undefined);
    /** 下一次重连是否走「短延迟」：服务端主动截断（truncated）时用 */
    const fastRetryRef = useRef<boolean>(false);
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
     * 与 streamedRef（已落屏字节数）比较：整段已落屏就丢弃，部分重叠就裁掉重复前缀。
     * 这样「HTTP 补历史」与「WebSocket history 帧」先后到达都不会重复刷屏。
     *
     * offset 与 streamedRef 都是**字节**偏移，裁剪也必须按字节来 ——
     * 用 text.slice() 会按 UTF-16 码元裁，日志里有中文时就会多裁/少裁。
     */
    const writeAt = useCallback(
      (offset: number, text: string) => {
        if (!text) return;
        const bytes = utf8Encoder.encode(text);
        const skip = streamedRef.current - offset;
        if (skip >= bytes.length) return; // 整段都已经落屏
        const chunk = skip > 0 ? bytes.subarray(skip) : bytes;
        writeData(utf8Decoder.decode(chunk));
        const end = offset + bytes.length;
        if (end > streamedRef.current) {
          streamedRef.current = end;
        }
      },
      [writeData],
    );

    const clear = useCallback(() => {
      termRef.current?.clear();
      streamedRef.current = offsetRef.current;
    }, []);

    /** 处理服务端帧 */
    const handleFrame = useCallback(
      (frame: WsFrame) => {
        onFrame?.(frame);
        switch (frame.type) {
          case 'history':
          case 'log': {
            const data = frame.data || '';
            const nextOffset =
              typeof frame.nextOffset === 'number' ? frame.nextOffset : undefined;
            if (data) {
              // 按 offset 去重：WebSocket 会从 offset 续传，但「HTTP 补历史」可能已经
              // 先写入了同样的内容，这里统一走 writeAt 裁掉重复部分。
              // 服务端会显式给出本帧的起始字节偏移；老版本服务端没这个字段，
              // 退化成「nextOffset - 本帧字节数」（注意是字节，不是 string.length）。
              const start =
                typeof frame.offset === 'number'
                  ? frame.offset
                  : nextOffset !== undefined
                    ? nextOffset - byteLength(data)
                    : streamedRef.current;
              writeAt(start, data);
            }
            if (nextOffset !== undefined) {
              offsetRef.current = nextOffset;
            }
            break;
          }
          case 'status':
            break;
          case 'truncated': {
            // 服务端把「读得慢」的订阅者踢掉了：运行还没结束，立刻带 offset 续上。
            if (typeof frame.nextOffset === 'number' && frame.nextOffset > offsetRef.current) {
              offsetRef.current = frame.nextOffset;
            }
            reconnectSoonRef.current();
            break;
          }
          case 'exit': {
            if (isRunFinished(frame.status)) {
              // 只有终态 exit 才是收工信号：服务端保证发它之前已把文件补完。
              doneRef.current = true;
              runOverRef.current = true;
              setState('closed');
              onExit?.(frame.status, frame.exitCode ?? null);
            } else {
              // 老版本服务端会用 exit 帧通知「订阅被踢」，此时运行并没有结束，
              // 直接当结束处理会让日志永久缺尾 —— 按未结束续传。
              reconnectSoonRef.current();
            }
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

    /** 建立连接。force=true 表示手动重连：即使运行已结束也允许再连一次把日志补齐 */
    const connect = useCallback(
      (force = false) => {
        const term = termRef.current;
        if (!term) return;
        if (closedRef.current) return;
        if (doneRef.current && !force) {
          setState('closed');
          return;
        }
        // 页面打开时运行就已经结束了（本实例没连过）：历史日志由父组件
        // 走 HTTP 分块拉全，这里不必再拉一遍，等用户点「重连」再连。
        if (runOverRef.current && !liveRef.current && !force) {
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
          if (!closedRef.current) {
            liveRef.current = true;
          }
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
          if (closedRef.current || doneRef.current) {
            setState('closed');
            return;
          }
          // 运行结束但还没拿到终态 exit 帧：继续按 offset 连，把尾巴补回来。
          scheduleReconnectRef.current();
        };
      },
      [handleFrame, onError, runId, writeData],
    );
    connectRef.current = connect;

    /**
     * 「服务端截断了本连接」时的处理：运行没结束，要尽快续上，不必等退避。
     *
     * 只置一个标记 + 关掉当前连接，真正的重连交给 onclose → scheduleReconnect：
     * 那样只有一条重连路径，不会出现「定时器 + onclose 各连一次」的双连接
     * （两个连接同时写同一个终端，去重状态会被互相干扰）。
     */
    const reconnectSoon = useCallback(() => {
      if (closedRef.current || doneRef.current) return;
      retryRef.current = 0;
      fastRetryRef.current = true;
      const ws = wsRef.current;
      if (!ws) {
        scheduleReconnectRef.current();
        return;
      }
      try {
        ws.close();
      } catch (e) {
        // ignore
      }
    }, []);
    reconnectSoonRef.current = reconnectSoon;

    /** 手动重连：不等退避，立刻再连一次（运行已结束时服务端会补完历史再发 exit） */
    const reconnect = useCallback(() => {
      retryRef.current = 0;
      if (timerRef.current) clearTimeout(timerRef.current);
      try {
        wsRef.current?.close();
      } catch (e) {
        // ignore
      }
      wsRef.current = null;
      connect(true);
    }, [connect]);

    /** 重载日志：清空终端与「已落屏字节数」，从 0 重新拉，专治终端空白/内容错乱 */
    const reload = useCallback(() => {
      try {
        termRef.current?.reset();
      } catch (e) {
        // ignore
      }
      streamedRef.current = 0;
      offsetRef.current = 0;
      retryRef.current = 0;
      overRetryRef.current = 0;
      fastRetryRef.current = false;
      doneRef.current = false;
      if (timerRef.current) clearTimeout(timerRef.current);
      try {
        wsRef.current?.close();
      } catch (e) {
        // ignore
      }
      wsRef.current = null;
      connect(true);
    }, [connect]);

    // 暴露给父组件（reconnect / reload 必须先定义，否则 eslint 的
    // no-use-before-define 会报错）。
    useImperativeHandle(ref, () => ({
      clear,
      scrollToBottom: () => {
        setAutoScroll(true);
        autoScrollRef.current = true;
        termRef.current?.scrollToBottom();
      },
      write: writeData,
      writeAt,
      reconnect,
      reload,
    }));

    /** 指数退避重连：普通断线 1s/2s/4s/8s…；服务端主动截断时从 250ms 起，同样封顶 10s */
    const scheduleReconnect = useCallback(() => {
      if (closedRef.current || doneRef.current) return;
      if (runOverRef.current) {
        // 运行已结束：日志不会再增长，只做有限次补收（文件可能已被清理），
        // 但必须允许补收 —— 否则「连接在 exit 帧到达前断掉」时日志会缺尾。
        if (!liveRef.current || overRetryRef.current >= MAX_OVER_RETRY) {
          setState('closed');
          return;
        }
        overRetryRef.current += 1;
      }
      const attempt = retryRef.current;
      const base = fastRetryRef.current ? TRUNCATED_RETRY_MS : 1000;
      fastRetryRef.current = false;
      retryRef.current = attempt + 1;
      const delay = Math.min(base * 2 ** attempt, MAX_BACKOFF);
      setState('reconnecting');
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        connectRef.current();
      }, delay);
    }, []);

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
      runOverRef.current = !active;
      doneRef.current = false;
      liveRef.current = active;
      overRetryRef.current = 0;
      fastRetryRef.current = false;
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

    // 运行「由未知/已结束变为活跃」时必须主动补一次建连。
    //
    // 父组件拿到 run 详情之前 active 就是 false —— 那是「还不知道」，
    // 不是「已经结束」。挂载时的 connect() 会按「结束」提前返回，而它只跑一次，
    // 于是之后再没人叫醒终端：日志只剩父组件那一次 HTTP 历史拉取（拉到没有新
    // 字节就停），整条流就此冻住。点「重新执行」跳到新运行时最明显 ——
    // 终端只显示开头几行，后面全丢（实测：运行详情还没加载完就挂载的终端从未建连）。
    useEffect(() => {
      if (!active) return;
      runOverRef.current = false;
      doneRef.current = false;
      overRetryRef.current = 0;
      retryRef.current = 0;
      if (wsRef.current) return; // 已经连着，别重复建连
      connectRef.current(true);
    }, [active]);

    // 运行结束（active=false）时的收尾。
    //
    // 这里**不能**直接 close WebSocket：服务端是在「补完文件」之后才发 exit 帧的，
    // 状态落库（父组件轮询到的 active=false）可能早于最后一帧到达，
    // 一 close 就把还在路上的尾巴丢了 —— 这正是「运行结束后日志不全」的成因。
    // 正确做法：连接还在就让它自然收完（终态 exit 帧会把它标成 done）；
    // 连接已经断了才主动再连一次，按 offset 把尾巴补回来。
    useEffect(() => {
      if (active) return;
      runOverRef.current = true;
      if (doneRef.current || closedRef.current) return;
      if (liveRef.current && !wsRef.current) {
        if (timerRef.current) clearTimeout(timerRef.current);
        retryRef.current = 0;
        connectRef.current(true);
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
              icon={<LinkOutlined />}
              onClick={reconnect}
              title="不等退避，立刻重新连接日志流"
            >
              重连
            </Button>
            <Button
              size="small"
              icon={<ReloadOutlined />}
              onClick={reload}
              title="清空终端并从第 0 字节重新拉取日志（终端空白或内容错乱时用）"
            >
              重载日志
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
