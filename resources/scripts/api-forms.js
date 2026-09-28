async function apiRequest(url, options) {
    const response = await fetch(url, options);
    const result = await response.json();
    if (!response.ok) throw new Error(result.message || "Ошибка запроса");
    return result.data;
}
for (const form of document.querySelectorAll(".api-create, .api-publish, .api-delete")) {
    form.addEventListener("submit", async event => {
        event.preventDefault();
        const button = form.querySelector('button[type="submit"]');
        button.disabled = true;
        try {
            if (form.classList.contains("api-create")) {
                await apiRequest("/api/tariffs", {method:"POST", body:new FormData(form)});
                location.href = "/tariffs/draft";
            } else if (form.classList.contains("api-publish")) {
                const data = Object.fromEntries(new FormData(form));
                data.price_per_month = Number(data.price_per_month);
                data.ram_gb = Number(data.ram_gb);
                await apiRequest(`/api/tariffs/${form.dataset.id}/publication`, {method:"PUT", headers:{"Content-Type":"application/json"},body:JSON.stringify(data)});
                location.href = `/tariffs/feed?id=${form.dataset.id}`;
            } else {
                await apiRequest(`/api/tariffs/${form.dataset.id}`, {method:"DELETE"});
                location.reload();
            }
        } catch (error) { alert(error.message); button.disabled = false; }
    });
}

for (const button of document.querySelectorAll(".api-like")) {
    button.addEventListener("click", async () => {
        button.disabled = true;
        try {
            const value = button.dataset.liked === "1" ? 0 : 1;
            const data = await apiRequest(`/api/tariffs/${button.dataset.id}/likes`, {method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({value})});
            button.dataset.liked = String(data.is_liked);
            button.setAttribute("aria-pressed", String(data.is_liked === 1));
            button.parentElement.querySelector(".like-count").textContent = data.like_count;
            button.parentElement.setAttribute("aria-label", `Лайков: ${data.like_count}`);
        } catch (error) { alert(error.message); }
        finally { button.disabled = false; }
    });
}
