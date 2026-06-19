import { LightningElement } from "lwc";
import * as api from "lightning/uiLayoutApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiLayoutApi";
  exports = Object.keys(api || {}).join(",");
}
