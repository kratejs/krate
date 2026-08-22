(function () {
    var sideBtn = document.getElementById("sidebar-toggle");
    var sideClose = document.getElementById("sidebar-close");
    var tocBtn = document.getElementById("toc-toggle");
    var sidebar = document.getElementById("sidebar");
    var toc = document.getElementById("toc");
    var sideOverlay = document.getElementById("sidebar-overlay");
    var tocCurrent = document.getElementById("toc-current");
    var themeBtn = document.getElementById("theme-toggle");
    var desktop = window.matchMedia("(min-width: 1280px)");
    var ticking = false;
    var lastScrollTop = 0;
    var currentDir = "down";
    var animProgress = { startLength: 0, endLength: 0, targetStart: 0, targetEnd: 0 };
    var currentPathLength = 0;
    var isAnimating = false;
    var visibleHeadings = Object.create(null);
    var headings = [];
    var tocLinks = [];
    var tocById = Object.create(null);
    var tocSvg = null;
    var tocRailPath = null;
    var tocLinePath = null;
    var tocCircle = null;
    var activeId = "";

    function setExpanded(button, open) {
        if (button) button.setAttribute("aria-expanded", open ? "true" : "false");
    }

    function normalizePath(value) {
        var path = value || "/";
        try {
            path = new URL(path, window.location.origin).pathname;
        } catch (error) {
            path = String(path).split("#")[0].split("?")[0];
        }
        path = path.replace(/\/index\.html$/, "/");
        if (path.length > 1) path = path.replace(/\/+$/, "");
        return path || "/";
    }

    function closeSidebar() {
        if (sidebar) sidebar.classList.remove("open");
        if (sideOverlay) sideOverlay.classList.remove("open");
        setExpanded(sideBtn, false);
    }

    function closeToc() {
        if (toc) toc.classList.remove("open");
        setExpanded(tocBtn, false);
    }

    function closeAll() {
        closeSidebar();
        closeToc();
    }

    function setSectionOpen(section, open) {
        var button = section.querySelector(":scope > .sidebar-section-row .sidebar-section-toggle");
        section.classList.toggle("open", open);
        section.classList.toggle("collapsed", !open);
        setExpanded(button, open);
    }

    function openActiveSidebarParents() {
        if (!sidebar) return;
        var currentPath = normalizePath(window.location.pathname);
        var links = sidebar.querySelectorAll("a[href]");

        for (var i = 0; i < links.length; i++) {
            var linkPath = normalizePath(links[i].getAttribute("href"));
            var isActive = linkPath === currentPath;
            links[i].classList.toggle("active", isActive);
            if (isActive) {
                var parent = links[i].closest(".sidebar-section");
                while (parent) {
                    parent.classList.add("active");
                    if (parent.classList.contains("sidebar-collapsible")) setSectionOpen(parent, true);
                    parent = parent.parentElement ? parent.parentElement.closest(".sidebar-section") : null;
                }
            }
        }
    }

    function initSidebar() {
        if (!sidebar) return;
        var sections = sidebar.querySelectorAll(".sidebar-collapsible");
        for (var i = 0; i < sections.length; i++) {
            setSectionOpen(sections[i], sections[i].classList.contains("open"));
        }

        sidebar.addEventListener("click", function (event) {
            var toggle = event.target.closest(".sidebar-section-toggle");
            if (toggle) {
                var section = toggle.closest(".sidebar-collapsible");
                if (section) setSectionOpen(section, !section.classList.contains("open"));
                return;
            }

            var link = event.target.closest("a[href]");
            if (link && !desktop.matches) closeSidebar();
        });

        openActiveSidebarParents();
    }

    function tocDepth(link) {
        var raw = parseInt(link.getAttribute("data-depth") || "2", 10);
        if (!isFinite(raw)) raw = link.classList.contains("toc-h3") ? 3 : 2;
        return Math.max(2, Math.min(raw, 4));
    }

    function ensureTocSvg(nav) {
        if (tocSvg) return;
        var ns = "http://www.w3.org/2000/svg";
        tocSvg = document.createElementNS(ns, "svg");
        tocSvg.setAttribute("class", "toc-indicator-svg");
        tocSvg.setAttribute("aria-hidden", "true");
        tocSvg.setAttribute("focusable", "false");
        tocRailPath = document.createElementNS(ns, "path");
        tocLinePath = document.createElementNS(ns, "path");
        tocCircle = document.createElementNS(ns, "circle");
        tocRailPath.setAttribute("class", "toc-indicator-rail");
        tocLinePath.setAttribute("class", "toc-indicator-line");
        tocCircle.setAttribute("class", "toc-indicator-circle");
        tocCircle.setAttribute("r", "2.6");
        tocSvg.appendChild(tocRailPath);
        tocSvg.appendChild(tocLinePath);
        tocSvg.appendChild(tocCircle);
        nav.appendChild(tocSvg);
    }

    function generateCurvedPath(points) {
        if (!points.length) return "";
        var d = "M " + points[0].x + " " + points[0].yStart + " L " + points[0].x + " " + points[0].yEnd;
        for (var i = 1; i < points.length; i++) {
            var prev = points[i - 1];
            var curr = points[i];
            if (Math.abs(prev.x - curr.x) > 0.5) {
                var midY = prev.yEnd + (curr.yStart - prev.yEnd) * 0.5;
                d += " C " + prev.x + " " + midY + ", " + curr.x + " " + midY + ", " + curr.x + " " + curr.yStart;
            } else {
                d += " L " + curr.x + " " + curr.yStart;
            }
            d += " L " + curr.x + " " + curr.yEnd;
        }
        return d;
    }

    function pointForLink(link, navRect) {
        var rect = link.getBoundingClientRect();
        var depthOffset = (tocDepth(link) - 2) * 12;
        return {
            x: 5 + depthOffset,
            yStart: Math.max(0, rect.top - navRect.top + 6),
            yEnd: Math.max(0, rect.bottom - navRect.top - 6),
            id: link.getAttribute("data-id") || "",
        };
    }

    function refreshVisibleHeadings() {
        var topLimit = 72;
        var bottomLimit = Math.max(topLimit + 120, window.innerHeight - 24);
        visibleHeadings = Object.create(null);
        for (var i = 0; i < headings.length; i++) {
            var rect = headings[i].getBoundingClientRect();
            if (rect.top < bottomLimit && rect.bottom > topLimit) visibleHeadings[headings[i].id] = true;
        }
    }

    function computeActiveHeading() {
        if (!headings.length) return "";
        var readingLine = (window.pageYOffset || document.documentElement.scrollTop || 0) + 96;
        var current = headings[0];

        for (var i = 0; i < headings.length; i++) {
            var top = headings[i].getBoundingClientRect().top + (window.pageYOffset || document.documentElement.scrollTop || 0);
            if (top <= readingLine) {
                current = headings[i];
            } else {
                break;
            }
        }

        return current ? current.id : "";
    }

    function updateLineCoords() {
        if (!tocLinks.length || !tocSvg || !tocRailPath || !tocLinePath) return;
        var nav = tocSvg.parentElement;
        var navRect = nav.getBoundingClientRect();
        var railPoints = [];
        var targetLinks = [];
        var activeLink = activeId ? tocById[activeId] : null;

        for (var i = 0; i < tocLinks.length; i++) {
            var link = tocLinks[i];
            railPoints.push(pointForLink(link, navRect));
            if (visibleHeadings[link.getAttribute("data-id")]) targetLinks.push(link);
        }

        if (activeLink && targetLinks.indexOf(activeLink) === -1) targetLinks.push(activeLink);
        targetLinks.sort(function (a, b) {
            return a.getBoundingClientRect().top - b.getBoundingClientRect().top;
        });

        var height = Math.max(nav.scrollHeight, navRect.height, 1);
        tocSvg.setAttribute("viewBox", "0 0 34 " + height.toFixed(1));
        tocSvg.style.height = height + "px";

        var railPath = generateCurvedPath(railPoints);
        tocRailPath.setAttribute("d", railPath);
        tocLinePath.setAttribute("d", railPath);

        try {
            currentPathLength = tocLinePath.getTotalLength();
        } catch (error) {
            currentPathLength = 0;
        }
        if (!currentPathLength || !targetLinks.length) {
            tocLinePath.style.strokeDasharray = "0 1";
            tocCircle.classList.remove("visible");
            return;
        }

        var targetPoints = [];
        for (var j = 0; j < targetLinks.length; j++) targetPoints.push(pointForLink(targetLinks[j], navRect));
        var startY = targetPoints[0].yStart;
        var endY = targetPoints[targetPoints.length - 1].yEnd;
        var startOffset = 0;
        var endOffset = currentPathLength;
        var foundStart = false;
        var steps = Math.max(80, Math.min(240, Math.round(currentPathLength / 2)));

        for (var step = 0; step <= steps; step++) {
            var len = (step / steps) * currentPathLength;
            var pt = tocLinePath.getPointAtLength(len);
            if (!foundStart && pt.y >= startY - 1) {
                startOffset = len;
                foundStart = true;
            }
            if (pt.y <= endY + 1) endOffset = len;
        }

        animProgress.targetStart = startOffset;
        animProgress.targetEnd = Math.max(startOffset, endOffset);
        if (!isAnimating) {
            if (animProgress.startLength === 0 && animProgress.endLength === 0) {
                animProgress.startLength = animProgress.targetStart;
                animProgress.endLength = animProgress.targetEnd;
            }
            isAnimating = true;
            renderLineLoop();
        }
    }

    function renderLineLoop() {
        if (!tocLinePath || !tocCircle || !currentPathLength) {
            isAnimating = false;
            return;
        }

        var ease = 0.22;
        animProgress.startLength += (animProgress.targetStart - animProgress.startLength) * ease;
        animProgress.endLength += (animProgress.targetEnd - animProgress.endLength) * ease;

        var s = Math.max(0, Math.min(currentPathLength, animProgress.startLength));
        var e = Math.max(s, Math.min(currentPathLength, animProgress.endLength));
        tocLinePath.style.strokeDasharray = (e - s) + " " + currentPathLength;
        tocLinePath.style.strokeDashoffset = "-" + s;

        try {
            var circleLen = currentDir === "down" ? e : s;
            var pt = tocLinePath.getPointAtLength(circleLen);
            tocCircle.setAttribute("cx", pt.x);
            tocCircle.setAttribute("cy", pt.y);
            tocCircle.classList.add("visible");
        } catch (error) {}

        var dist = Math.abs(animProgress.targetStart - animProgress.startLength) + Math.abs(animProgress.targetEnd - animProgress.endLength);
        if (dist < 0.1) {
            animProgress.startLength = animProgress.targetStart;
            animProgress.endLength = animProgress.targetEnd;
            isAnimating = false;
        } else {
            window.requestAnimationFrame(renderLineLoop);
        }
    }

    function updateVisibleTocLinks() {
        for (var i = 0; i < tocLinks.length; i++) {
            var id = tocLinks[i].getAttribute("data-id");
            tocLinks[i].classList.toggle("in-view", !!visibleHeadings[id]);
        }
        updateLineCoords();
    }

    function setActiveToc(id) {
        if (!id) {
            updateVisibleTocLinks();
            return;
        }
        activeId = id;
        for (var i = 0; i < tocLinks.length; i++) {
            var link = tocLinks[i];
            var linkId = link.getAttribute("data-id");
            var isActive = linkId === id;
            link.classList.toggle("active", isActive);
            if (isActive && tocCurrent) tocCurrent.textContent = (link.textContent || "").trim();
        }
        updateVisibleTocLinks();
    }

    function scheduleTocUpdate() {
        if (ticking) return;
        ticking = true;
        window.requestAnimationFrame(function () {
            ticking = false;
            var st = window.pageYOffset || document.documentElement.scrollTop || 0;
            currentDir = st >= lastScrollTop ? "down" : "up";
            lastScrollTop = st <= 0 ? 0 : st;
            refreshVisibleHeadings();
            setActiveToc(computeActiveHeading());
        });
    }

    function initTocTracking() {
        if (!toc) return;
        var nav = toc.querySelector(".toc-nav");
        if (!nav) return;
        tocLinks = Array.prototype.slice.call(nav.querySelectorAll(".toc-link[href^='#']"));
        if (!tocLinks.length) return;
        ensureTocSvg(nav);

        for (var i = 0; i < tocLinks.length; i++) {
            var href = tocLinks[i].getAttribute("href") || "";
            var id = decodeURIComponent(href.slice(1));
            tocLinks[i].setAttribute("data-id", id);
            tocById[id] = tocLinks[i];
            var heading = document.getElementById(id);
            if (heading) headings.push(heading);
        }

        if (tocCurrent) tocCurrent.textContent = (tocLinks[0].textContent || "On this page").trim();
        refreshVisibleHeadings();
        setActiveToc(window.location.hash ? decodeURIComponent(window.location.hash.slice(1)) : computeActiveHeading() || (headings[0] && headings[0].id));

        if ("IntersectionObserver" in window) {
            var observer = new IntersectionObserver(scheduleTocUpdate, {
                rootMargin: "-2% 0px -2% 0px",
                threshold: 0,
            });
            for (var k = 0; k < headings.length; k++) observer.observe(headings[k]);
        }

        window.addEventListener("scroll", scheduleTocUpdate, { passive: true });
        window.addEventListener("resize", scheduleTocUpdate);
        window.addEventListener("hashchange", function () {
            if (window.location.hash) {
                refreshVisibleHeadings();
                setActiveToc(decodeURIComponent(window.location.hash.slice(1)));
            }
        });

        for (var n = 0; n < tocLinks.length; n++) {
            tocLinks[n].addEventListener("click", function () {
                if (!desktop.matches) closeToc();
                window.setTimeout(scheduleTocUpdate, 80);
            });
        }

        window.setTimeout(scheduleTocUpdate, 120);
    }

    if (sideBtn && sidebar) {
        sideBtn.addEventListener("click", function () {
            var isOpen = !sidebar.classList.contains("open");
            sidebar.classList.toggle("open", isOpen);
            if (sideOverlay) sideOverlay.classList.toggle("open", isOpen);
            setExpanded(sideBtn, isOpen);
            if (isOpen) closeToc();
        });
    }

    if (sideClose) sideClose.addEventListener("click", closeSidebar);
    if (sideOverlay) sideOverlay.addEventListener("click", closeSidebar);

    if (tocBtn && toc) {
        tocBtn.addEventListener("click", function () {
            var isOpen = !toc.classList.contains("open");
            toc.classList.toggle("open", isOpen);
            setExpanded(tocBtn, isOpen);
            if (isOpen) closeSidebar();
            window.setTimeout(scheduleTocUpdate, 120);
        });
    }

    window.addEventListener("keydown", function (event) {
        if (event.key === "Escape") closeAll();
    });

    if (desktop.addEventListener) {
        desktop.addEventListener("change", function (event) {
            if (event.matches) closeAll();
            window.setTimeout(scheduleTocUpdate, 80);
        });
    } else if (desktop.addListener) {
        desktop.addListener(function (event) {
            if (event.matches) closeAll();
            window.setTimeout(scheduleTocUpdate, 80);
        });
    }

    initSidebar();
    initTocTracking();
    if (desktop.matches) closeAll();

    function setTheme(theme, persist) {
        document.documentElement.setAttribute("data-theme", theme);
        if (themeBtn) {
            themeBtn.setAttribute("aria-label", theme === "dark" ? "Switch to light mode" : "Switch to dark mode");
            themeBtn.setAttribute("data-theme-state", theme);
        }
        if (persist) {
            try {
                localStorage.setItem("theme", theme);
            } catch (error) {}
        }
    }

    try {
        var savedTheme = localStorage.getItem("theme");
        if (savedTheme === "dark" || savedTheme === "light") {
            setTheme(savedTheme, false);
        } else if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
            setTheme("dark", false);
        } else {
            setTheme("light", false);
        }
    } catch (error) {
        setTheme(window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light", false);
    }

    if (themeBtn) {
        themeBtn.addEventListener("click", function () {
            var current = document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
            setTheme(current === "dark" ? "light" : "dark", true);
            window.setTimeout(scheduleTocUpdate, 80);
        });
    }
})();