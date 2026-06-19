import { LightningElement } from "lwc";
import * as api from "lightning/mobileCapabilities";

export default class Oracle extends LightningElement {
  label = "lightning/mobileCapabilities";
  exports = Object.keys(api || {}).join(",");
}
