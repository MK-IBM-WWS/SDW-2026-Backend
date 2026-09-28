// Install before media elements are parsed: resource errors do not bubble.
(() => {
    document.addEventListener("error", event => {
        let media = event.target;
        if (media instanceof HTMLSourceElement) media = media.parentElement;
        if (!(media instanceof HTMLImageElement || media instanceof HTMLVideoElement)) return;
        const fallback = media.dataset.mediaFallback;
        // Stop if the example itself is unavailable; never create an error loop.
        if (!fallback || media.dataset.fallbackApplied === "1") return;
        media.dataset.fallbackApplied = "1";
        if (media instanceof HTMLVideoElement) {
            media.querySelectorAll("source").forEach(source => source.remove());
            media.src = fallback;
            media.load();
            if (media.autoplay) media.play().catch(() => {});
        } else {
            media.removeAttribute("srcset");
            media.src = fallback;
        }
    }, true);
})();
