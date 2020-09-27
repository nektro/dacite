/**
 */
//
const observer = new IntersectionObserver((changes) => {
    changes.forEach((change) => {
        if (change.isIntersecting) {
            const parent = change.target.parentElement.getAttribute("data-src");
            const ext = parent.substring(parent.lastIndexOf(".")+1);
            const icon = `https://unpkg.com/file-icon-vectors@1.0.0/dist/icons/vivid/${ext}.svg`;
            const images = ["png", "jpg", "jpeg", "gif"];
            const newsrc = images.includes(ext) ? parent : icon;
            change.target.setAttribute("src", newsrc);
            observer.unobserve(change.target);
        }
    });
});
//
customElements.define("x-image", class extends HTMLElement {
    constructor() {
        super();
    }
    connectedCallback() {
        const i = document.createElement("img");
        observer.observe(i);
        this.appendChild(i);
    }
});
