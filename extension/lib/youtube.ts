// YouTube 视频页识别：与后端 isYouTubeVideoPage（internal/service/youtube.go）
// 同一口径——watch?v= / shorts / live 回放 / youtu.be 短链算视频页，播放
// 列表与频道页不算。YouTube 播放媒体走 SABR/UMP（嗅探层不可见），弹窗
// 的专用卡片靠页面地址本身识别视频页，链接交给 Knot 的 yt-dlp 管线。

const YT_HOSTS = new Set([
  'www.youtube.com',
  'm.youtube.com',
  'music.youtube.com',
  'youtube.com',
  'youtu.be',
  'www.youtu.be',
]);

export function isYouTubeVideoPage(raw: string): boolean {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return false;
  }
  const host = u.hostname.toLowerCase();
  if (!YT_HOSTS.has(host)) return false;
  if (host === 'youtu.be' || host === 'www.youtu.be') {
    const p = u.pathname.replace(/^\/+|\/+$/g, '');
    return p !== '' && !p.includes('/');
  }
  if (u.pathname.startsWith('/watch')) return u.searchParams.get('v') !== '';
  if (u.pathname.startsWith('/shorts/') || u.pathname.startsWith('/live/')) return true;
  return false;
}
