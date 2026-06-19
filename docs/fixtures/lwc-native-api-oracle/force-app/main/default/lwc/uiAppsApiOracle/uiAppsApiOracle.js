import { LightningElement, wire } from "lwc";
import { getNavItems } from "lightning/uiAppsApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiAppsApi";
  @wire(getNavItems, {}) navItems;
}
