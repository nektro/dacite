/**
 */
//
function create_element(name, attrs, children) {
    const ele = document.createElement(name);
    (attrs || []).forEach((v) => { ele.setAttribute(v[0], v[1]); });
    (children || []).forEach((v) => { ele.appendChild(v); });
    return ele;
}
function dcTN(x) {
    return document.createTextNode(x);
}
(function() {
    window.addEventListener("load", () => {
        if (document.body.dataset.page === "upload") {
            const pr = document.querySelector("progress");
            document.forms.upload.addEventListener("submit", async (e) => {
                e.preventDefault();
                const table = document.getElementById("image-list");
                pr.max = e.target.files.files.length;
                pr.value = 0;
                for (const item of e.target.files.files) {
                    const fd = new FormData();
                    fd.append("image", item);
                    await fetch("./b/upload", { method:"post", credentials:"include", body:fd })
                    .then((x) => x.json())
                    .then((x) => {
                        if (x.hash !== undefined) {
                            const original = x.original ? "positive" : "negative";
                            table.prepend(create_element("tr", [], [
                                create_element("td", [], [
                                    create_element("a", [["href","./p/"+x.hash],["target","_none"]], [dcTN(x.hash)]),
                                ]),
                                create_element("td", [["class",original]], [dcTN(x.original.toString())]),
                            ]));
                        }
                        pr.value += 1;
                    });
                }
            });
        }
    });
})();
