(function () {
  const root = document.getElementById("github-download-count");
  if (!root) return;

  const valueEl = root.querySelector(".md-download-count__value");
  if (!valueEl) return;

  const repo = "cuxtud/morpheus-migration-utility";

  function setCount(n) {
    valueEl.textContent = Number(n).toLocaleString();
    root.setAttribute(
      "aria-label",
      Number(n).toLocaleString() + " GitHub release downloads"
    );
  }

  function sumReleaseDownloads(releases) {
    let total = 0;
    for (const release of releases) {
      for (const asset of release.assets || []) {
        total += asset.download_count || 0;
      }
    }
    return total;
  }

  fetch(
    "https://api.github.com/repos/" + repo + "/releases?per_page=100",
    { headers: { Accept: "application/vnd.github+json" } }
  )
    .then(function (r) {
      if (!r.ok) throw new Error("github api");
      return r.json();
    })
    .then(function (releases) {
      setCount(sumReleaseDownloads(releases));
    })
    .catch(function () {
      return fetch(
        "https://img.shields.io/github/downloads/" + repo + "/total.json"
      )
        .then(function (r) {
          if (!r.ok) throw new Error("shields");
          return r.json();
        })
        .then(function (data) {
          const n = parseInt(data.message, 10);
          if (isNaN(n)) throw new Error("shields parse");
          setCount(n);
        });
    })
    .catch(function () {
      valueEl.textContent = "—";
      root.setAttribute("aria-label", "Release download count unavailable");
    });
})();
