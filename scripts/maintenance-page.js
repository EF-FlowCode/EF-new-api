// Only probes status. Never replays a request or refreshes a page automatically.
async function checkMaintenance() {
  try {
    const response = await fetch('/__maintenance/status', {
      cache: 'no-store', signal: AbortSignal.timeout(5000),
    });
    const status = response.ok ? await response.json() : null;
    if (status?.maintenance === false) {
      document.getElementById('title').textContent = '服务已恢复';
      document.getElementById('message').textContent = '维护已完成，可以继续使用。';
      document.getElementById('status').textContent = '点击下方按钮返回原页面。';
      document.getElementById('return').hidden = false;
      return;
    }
  } catch {
    document.getElementById('status').textContent = '暂时无法检查恢复状态，稍后会继续检查。';
  }
  setTimeout(checkMaintenance, 15000);
}
document.getElementById('return').addEventListener('click', () => location.reload());
setTimeout(checkMaintenance, 3000);
