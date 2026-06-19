import { LightningElement } from "lwc";
import * as api from "lightning/uiListApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiListApi";
  exports = Object.keys(api || {}).join(",");
}
