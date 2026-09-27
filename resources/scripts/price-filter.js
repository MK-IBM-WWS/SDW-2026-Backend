const priceSteps = [
    { value: "", label: "без ограничения" },
    { value: "1000", label: "до 1 000 ₽" },
    { value: "10000", label: "до 10 000 ₽" },
    { value: "500000", label: "до 500 000 ₽" },
];

const slider = document.getElementById("priceStep");
const hiddenPrice = document.getElementById("priceLimit");
const caption = document.getElementById("priceCaption");
const form = slider.form;
let submitTimer;

function updatePriceFilter() {
    const selected = priceSteps[Number(slider.value)];
    hiddenPrice.value = selected.value;
    caption.textContent = selected.label;
}

slider.addEventListener("input", () => {
    updatePriceFilter();
    clearTimeout(submitTimer);
    submitTimer = setTimeout(() => form.requestSubmit(), 250);
});
updatePriceFilter();
