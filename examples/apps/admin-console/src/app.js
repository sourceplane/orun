const panel = document.querySelector(".panel");

if (panel) {
  const stamp = document.createElement("p");
  stamp.className = "timestamp";
  stamp.textContent = "Managed in Cloudflare through Terraform after Gluon planning.";
  panel.append(stamp);
}