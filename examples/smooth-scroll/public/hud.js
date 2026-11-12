(function () {
  function bind() {
    var lenis = window.lenis;
    var state = document.getElementById('hud-state');
    if (!state) return;
    if (!lenis) {
      state.textContent = 'not active';
      return;
    }
    state.textContent = 'active v' + (window.lenisVersion || '');
    if (window.__hudBound) return;
    window.__hudBound = true;
    lenis.on('scroll', function (l) {
      var s = document.getElementById('hud-scroll');
      var v = document.getElementById('hud-velocity');
      if (s) s.textContent = Math.round(l.scroll);
      if (v) v.textContent = l.velocity.toFixed(2);
    });
  }
  window.addEventListener('load', bind);
  window.addEventListener('krate:navigate-end', bind);
})();
