// Preview stays in this browser tab. Only the selected file names are submitted.
function bindPreview(kind) {
    const input = document.getElementById(`${kind}_file`);
    const preview = document.getElementById(`${kind}_preview`);
    const prompt = document.getElementById(`${kind}_prompt`);
    const name = document.getElementById(`${kind}_name`);
    let objectURL;

    input.addEventListener("change", () => {
        if (objectURL) URL.revokeObjectURL(objectURL);
        const file = input.files[0];
        name.value = file ? file.name : "";
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
