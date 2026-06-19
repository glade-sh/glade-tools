import { LightningElement } from "lwc";
import * as api from "lightning/uiObjectInfoApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiObjectInfoApi";
  exports = Object.keys(api || {}).join(",");
}
