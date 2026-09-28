// Local preview; FormData uploads the actual selected files to the API.
function bindPreview(kind) {
    const input = document.getElementById(`${kind}_file`);
    const preview = document.getElementById(`${kind}_preview`);
    const prompt = document.getElementById(`${kind}_prompt`);
    let objectURL;

    input.addEventListener("change", () => {
        if (objectURL) URL.revokeObjectURL(objectURL);
        const file = input.files[0];
        preview.hidden = !file;
        prompt.hidden = Boolean(file);
        if (file) {
            objectURL = URL.createObjectURL(file);
            preview.src = objectURL;
            if (kind === "video") preview.load();
        } else {
            preview.removeAttribute("src");
            objectURL = undefined;
        }
    });

    window.addEventListener("pagehide", () => {
        if (objectURL) URL.revokeObjectURL(objectURL);
    });
}

bindPreview("video");
bindPreview("image");
