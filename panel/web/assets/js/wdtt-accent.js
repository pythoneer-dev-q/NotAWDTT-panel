(function () {
  'use strict';
  var key = 'wdttAccentColor';
  var colors = {
    orange: '#f46b17',
    blue: '#3488f5',
    green: '#20a783',
    violet: '#9467e8',
    rose: '#e75b7c'
  };
  function selected() {
    try {
      var value = localStorage.getItem(key);
      return colors[value] ? value : 'orange';
    } catch (_) {
      return 'orange';
    }
  }
  function apply(name) {
    if (!colors[name]) name = 'orange';
    var hex = colors[name];
    var rgb = [1, 3, 5].map(function (at) { return parseInt(hex.slice(at, at + 2), 16); });
    var root = document.documentElement.style;
    root.setProperty('--wdtt-accent', hex);
    root.setProperty('--wdtt-accent-hover', 'rgb(' + rgb.map(function (n) { return Math.min(255, Math.round(n + (255 - n) * .18)); }).join(',') + ')');
    root.setProperty('--wdtt-accent-soft', 'rgba(' + rgb.join(',') + ',.12)');
    root.setProperty('--wdtt-accent-rgb', rgb.join(','));
    return name;
  }
  window.WdttAccent = {
    colors: colors,
    get: selected,
    set: function (name) {
      name = apply(name);
      try { localStorage.setItem(key, name); } catch (_) {}
      return name;
    }
  };
  apply(selected());
}());
