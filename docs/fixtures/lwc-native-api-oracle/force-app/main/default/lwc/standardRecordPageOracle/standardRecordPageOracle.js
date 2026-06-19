import { LightningElement } from "lwc";
import { NavigationMixin } from "lightning/navigation";

export default class Oracle extends NavigationMixin(LightningElement) {
  label = "standard__recordPage";
  connectedCallback() {
    this[NavigationMixin.GenerateUrl]({ type: "standard__recordPage", attributes: {}, state: { c__oracle: "1" } })
      .then((url) => { this.url = url; })
      .catch((error) => { this.url = error?.message || "navigation unavailable"; });
  }
}
