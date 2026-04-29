const panel = document.querySelector(".panel");

if (panel) {
  const stamp = document.createElement("p");
  stamp.className = "timestamp";
  stamp.textContent = `Build source: apps/web-console at ${new Date().toISOString()}`;
  panel.append(stamp);
}